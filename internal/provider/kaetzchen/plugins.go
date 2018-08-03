// plugins.go - plugin system for kaetzchen services
// Copyright (C) 2018  David Stainton.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

// Package kaetzchen implements support for provider side auto-responder
// agents.
package kaetzchen

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/hashicorp/go-plugin"
	"github.com/katzenpost/core/monotime"
	sConstants "github.com/katzenpost/core/sphinx/constants"
	"github.com/katzenpost/core/worker"
	"github.com/katzenpost/server/internal/glue"
	"github.com/katzenpost/server/internal/packet"
	"golang.org/x/text/secure/precis"
	"gopkg.in/eapache/channels.v1"
	"gopkg.in/op/go-logging.v1"
)

// KaetzchenService is the name of our Kaetzchen plugins.
var KaetzchenService = "kaetzchen"

// PluginMap is the map of plugins we can dispense.
var PluginMap = map[string]plugin.Plugin{
	KaetzchenService: &KaetzchenPlugin{},
}

// PluginKaetzchenWorker is similar to Kaetzchen worker but uses
// the go-plugin system to implement services in external programs.
// These plugins can be written in any language as long as it speaks gRPC
// over unix domain socket.
type PluginKaetzchenWorker struct {
	sync.Mutex
	worker.Worker

	glue glue.Glue
	log  *logging.Logger

	pluginChan map[[sConstants.RecipientIDLength]byte]*channels.InfiniteChannel
}

func (k *PluginKaetzchenWorker) OnKaetzchen(pkt *packet.Packet) {
	handlerCh, ok := k.pluginChan[pkt.Recipient.ID]
	if !ok {
		k.log.Debugf("Failed to find handler. Dropping Kaetzchen request: %v", pkt.ID)
		return
	}
	handlerCh.In() <- pkt
}

func (k *PluginKaetzchenWorker) worker(recipient [sConstants.RecipientIDLength]byte, pluginClient KaetzchenPluginInterface) {
	// Kaetzchen delay is our max dwell time.
	maxDwell := time.Duration(k.glue.Config().Debug.KaetzchenDelay) * time.Millisecond

	defer k.log.Debugf("Halting Kaetzchen worker.")
	// XXX defer pluginClient.Kill()

	handlerCh, ok := k.pluginChan[recipient]
	if !ok {
		k.log.Debugf("Failed to find handler. Dropping Kaetzchen request: %v", recipient)
		return
	}
	ch := handlerCh.Out()

	for {
		var pkt *packet.Packet
		select {
		case <-k.HaltCh():
			k.log.Debugf("Terminating gracefully.")
			return
		case e := <-ch:
			pkt = e.(*packet.Packet)
			if dwellTime := monotime.Now() - pkt.DispatchAt; dwellTime > maxDwell {
				k.log.Debugf("Dropping packet: %v (Spend %v in queue)", pkt.ID, dwellTime)
				pkt.Dispose()
				continue
			}
		}

		k.processKaetzchen(pkt, pluginClient)
	}
}

func (k *PluginKaetzchenWorker) processKaetzchen(pkt *packet.Packet, pluginClient KaetzchenPluginInterface) {
	defer pkt.Dispose()

	ct, surb, err := packet.ParseForwardPacket(pkt)
	if err != nil {
		k.log.Debugf("Dropping Kaetzchen request: %v (%v)", pkt.ID, err)
		return
	}

	var resp []byte
	respStr, err := pluginClient.OnRequest(string(ct))
	switch {
	case err == nil:
	case err == ErrNoResponse:
		k.log.Debugf("Processed Kaetzchen request: %v (No response)", pkt.ID)
		return
	default:
		k.log.Debugf("Failed to handle Kaetzchen request: %v (%v)", pkt.ID, err)
		return
	}
	resp = []byte(respStr)

	// Iff there is a SURB, generate a SURB-Reply and schedule.
	if surb != nil {
		// Prepend the response header.
		resp = append([]byte{0x01, 0x00}, resp...)

		respPkt, err := packet.NewPacketFromSURB(pkt, surb, resp)
		if err != nil {
			k.log.Debugf("Failed to generate SURB-Reply: %v (%v)", pkt.ID, err)
			return
		}

		k.log.Debugf("Handing off newly generated SURB-Reply: %v (Src:%v)", respPkt.ID, pkt.ID)
		k.glue.Scheduler().OnPacket(respPkt)
	} else if resp != nil {
		// This is silly and I'm not sure why anyone will do this, but
		// there's nothing that can be done at this point, the Kaetzchen
		// implementation should have caught this.
		k.log.Debugf("Kaetzchen message: %v (Has reply but no SURB)", pkt.ID)
	}
}

func (k *PluginKaetzchenWorker) IsKaetzchen(recipient [sConstants.RecipientIDLength]byte) bool {
	_, ok := k.pluginChan[recipient]
	return ok
}

func (k *PluginKaetzchenWorker) launch(command string) (KaetzchenPluginInterface, error) {
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: Handshake,
		Plugins:         PluginMap,
		Cmd:             exec.Command("sh", "-c", command),
		AllowedProtocols: []plugin.Protocol{
			plugin.ProtocolGRPC},
	})

	// Connect via RPC
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, err
	}

	// Request the plugin
	raw, err := rpcClient.Dispense(KaetzchenService)
	if err != nil {
		client.Kill()
		return nil, err
	}
	service, ok := raw.(KaetzchenPluginInterface)
	if !ok {
		client.Kill()
		return nil, errors.New("type assertion failure for KaetzchenPluginInterface")
	}
	return service, err
}

func NewPluginKaetzchenWorker(glue glue.Glue) (*PluginKaetzchenWorker, error) {

	kaetzchenWorker := PluginKaetzchenWorker{
		glue:       glue,
		log:        glue.LogBackend().GetLogger("kaetzchen_worker"),
		pluginChan: make(map[[sConstants.RecipientIDLength]byte]*channels.InfiniteChannel),
	}

	capaMap := make(map[string]bool)

	for _, pluginConf := range glue.Config().Provider.PluginKaetzchen {

		// Ensure no duplicates.
		capa := pluginConf.Capability
		if capa == "" {
			return nil, errors.New("kaetzchen plugin capability cannot be empty string")
		}
		if pluginConf.Disable {
			kaetzchenWorker.log.Noticef("Skipping disabled Kaetzchen: '%v'.", capa)
			continue
		}
		if capaMap[capa] {
			return nil, fmt.Errorf("provider: Kaetzchen '%v' registered more than once", capa)
		}

		// Sanitize the endpoint.
		if pluginConf.Endpoint == "" {
			return nil, fmt.Errorf("provider: Kaetzchen: '%v' provided no endpoint", capa)
		} else if epNorm, err := precis.UsernameCaseMapped.String(pluginConf.Endpoint); err != nil {
			return nil, fmt.Errorf("provider: Kaetzchen: '%v' invalid endpoint: %v", capa, err)
		} else if epNorm != pluginConf.Endpoint {
			return nil, fmt.Errorf("provider: Kaetzchen: '%v' invalid endpoint, not normalized", capa)
		}
		rawEp := []byte(pluginConf.Endpoint)
		if len(rawEp) == 0 || len(rawEp) > sConstants.RecipientIDLength {
			return nil, fmt.Errorf("provider: Kaetzchen: '%v' invalid endpoint, length out of bounds", capa)
		}

		//
		var endpoint [sConstants.RecipientIDLength]byte
		copy(endpoint[:], rawEp)
		kaetzchenWorker.pluginChan[endpoint] = channels.NewInfiniteChannel()

		// Start the plugin clients.
		for i := 0; i < pluginConf.MaxConcurrency; i++ {
			kaetzchenWorker.log.Noticef("Starting Kaetzchen plugin client: %s %d", capa, i)
			pluginClient, err := kaetzchenWorker.launch(pluginConf.Command)
			if err != nil {
				kaetzchenWorker.log.Error("Failed to start a plugin client.")
				return nil, err
			}

			// Start the worker.
			worker := func() {
				kaetzchenWorker.worker(endpoint, pluginClient)
			}
			kaetzchenWorker.Go(worker)
		}

		capaMap[capa] = true
	}

	return &kaetzchenWorker, nil
}
