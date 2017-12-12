// pki.go - Katzenpost server PKI interface.
// Copyright (C) 2017  Yawning Angel.
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

package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	nClient "github.com/katzenpost/authority/nonvoting/client"
	"github.com/katzenpost/core/crypto/ecdh"
	"github.com/katzenpost/core/crypto/eddsa"
	"github.com/katzenpost/core/epochtime"
	cpki "github.com/katzenpost/core/pki"
	"github.com/katzenpost/core/sphinx/constants"
	"github.com/katzenpost/core/wire"
	"github.com/katzenpost/core/worker"
	"github.com/katzenpost/server/internal/pkicache"
	"github.com/op/go-logging"
)

type pki struct {
	sync.RWMutex
	worker.Worker

	s    *Server
	log  *logging.Logger
	impl cpki.Client

	docs               map[uint64]*pkicache.Entry
	lastPublishedEpoch uint64
	lastWarnedEpoch    uint64
}

func (p *pki) startWorker() {
	p.Go(p.worker)
}

func (p *pki) worker() {
	const initialSpawnDelay = 5 * time.Second

	timer := time.NewTimer(initialSpawnDelay)
	defer func() {
		p.log.Debugf("Halting PKI worker.")
		timer.Stop()
	}()

	if p.impl == nil {
		p.log.Warningf("No implementation is configured, disabling PKI interface.")
		return
	}
	pkiCtx, cancelFn := context.WithCancel(context.Background())
	go func() {
		select {
		case <-p.HaltCh():
			cancelFn()
		case <-pkiCtx.Done():
		}
	}()
	isCanceled := func() bool {
		select {
		case <-pkiCtx.Done():
			return true
		default:
			return false
		}
	}

	// Note: The worker's start is delayed till after the Server's connector
	// is initialized, so that force updating the outgoing connection table
	// is guaranteed to work.

	for {
		const recheckInterval = 1 * time.Minute

		timerFired := false
		select {
		case <-p.HaltCh():
			p.log.Debugf("Terminating gracefully.")
			return
		case <-pkiCtx.Done():
			return
		case <-timer.C:
			timerFired = true
		}
		if !timerFired && !timer.Stop() {
			<-timer.C
		}

		// Fetch the PKI documents as required.
		didUpdate := false
		for _, epoch := range p.documentsToFetch() {
			d, err := p.impl.Get(pkiCtx, epoch)
			if isCanceled() {
				// Canceled mid-fetch.
				return
			}
			if err != nil {
				p.log.Warningf("Failed to fetch PKI for epoch %v: %v", epoch, err)
				continue
			}

			ent, err := pkicache.New(d, p.s.identityKey.PublicKey(), p.s.cfg.Server.IsProvider)
			if err != nil {
				p.log.Warningf("Failed to generate PKI cache for epoch %v: %v", epoch, err)
				continue
			}
			if err = p.validateCacheEntry(ent); err != nil {
				p.log.Warningf("Generated PKI cache is invalid: %v", err)
			}
			p.Lock()
			p.docs[epoch] = ent
			p.Unlock()
			didUpdate = true
		}
		if didUpdate {
			// Dispose of the old PKI documents.
			p.pruneDocuments()

			// If the PKI document map changed, kick the connector worker.
			p.s.connector.forceUpdate()
		}

		// Check to see if we need to publish the descriptor, and do so, along
		// with all the key rotation bits.
		err := p.publishDescriptorIfNeeded(pkiCtx)
		if isCanceled() {
			// Canceled mid-post
			return
		}
		if err != nil {
			p.log.Warningf("Failed to post to PKI: %v", err)
		}

		timer.Reset(recheckInterval)
	}
}

func (p *pki) validateCacheEntry(ent *pkicache.Entry) error {
	// This just does light-weight validation on self, primarily to catch
	// dumb bugs.  Anything more is somewhat silly because authorities are
	// a trust root, and no amount of checking here will save us if the
	// authorities are malicious.
	desc := ent.Self()
	if desc.Name != p.s.cfg.Server.Identifier {
		return fmt.Errorf("self Name field does not match Identifier")
	}
	if !desc.IdentityKey.Equal(p.s.identityKey.PublicKey()) {
		return fmt.Errorf("self identity key mismatch")
	}
	if !desc.LinkKey.Equal(p.s.linkKey.PublicKey()) {
		return fmt.Errorf("self link key mismatch")
	}
	return nil
}

func (p *pki) pruneDocuments() {
	now, _, _ := epochtime.Now()

	p.Lock()
	defer p.Unlock()
	for epoch := range p.docs {
		if epoch < now {
			p.log.Debugf("Discarding PKI for epoch: %v", epoch)
			delete(p.docs, epoch)
		}
		if epoch > now+1 {
			// This should NEVER happen.
			p.log.Debugf("Far future PKI document exists, clock ran backwards?: %v", epoch)
		}
	}
}

func (p *pki) publishDescriptorIfNeeded(pkiCtx context.Context) error {
	const publishDeadline = 3600 * time.Second

	epoch, _, till := epochtime.Now()
	doPublishEpoch := uint64(0)
	switch p.lastPublishedEpoch {
	case 0:
		// Initial startup.  Regardless of the deadline, publish.
		p.log.Debugf("Initial startup or correcting for time jump.")
		doPublishEpoch = epoch
	case epoch:
		// Check the deadline for the next publication time.
		if till > publishDeadline {
			p.log.Debugf("Within the publication time for epoch: %v", epoch+1)
			doPublishEpoch = epoch + 1
			break
		}

		// Well, we appeared to have missed the publication deadline for the
		// next epoch, so give up till the transition.
		if p.lastWarnedEpoch != epoch {
			// Debounce this so we don't spam the log.
			p.lastWarnedEpoch = epoch
			return fmt.Errorf("missed publication deadline for epoch: %v", epoch+1)
		}
		return nil
	case epoch + 1:
		// The next epoch has been published.
		return nil
	default:
		// What the fuck?  The last descriptor that we published is a time
		// that we don't recognize.  The system's civil time probably jumped,
		// even though the assumption is that all nodes run NTP.
		p.log.Warningf("Last published epoch %v is wildly disjointed from %v.", p.lastPublishedEpoch, epoch)

		// I don't even know what the sane thing to do here is, just treat it
		// as if the node's just started and publish for the current I guess.
		doPublishEpoch = epoch
	}

	// Note: Why, yes I *could* cache the descriptor and save a trivial amount
	// of time and CPU, but this is invoked infrequently enough that it's
	// probably not worth it.

	// Generate the non-key parts of the descriptor.
	desc := &cpki.MixDescriptor{
		Name:        p.s.cfg.Server.Identifier,
		IdentityKey: p.s.identityKey.PublicKey(),
		LinkKey:     p.s.linkKey.PublicKey(),
		Addresses:   p.s.cfg.Server.Addresses,
	}
	if p.s.cfg.Server.IsProvider {
		// Only set the layer if the node is a provider.  Otherwise, nodes
		// shouldn't be self assigning this.
		desc.Layer = cpki.LayerProvider
	}
	desc.MixKeys = make(map[uint64]*ecdh.PublicKey)

	// Ensure that there are mix keys for the epochs [e, ..., e+2],
	// assuming that key rotation isn't disabled, and fill them into
	// the descriptor.
	if p.s.cfg.Debug.DisableKeyRotation {
		// In the static mix key case, just publish the static keys for
		// all the epochs.
		staticKey := p.s.mixKeys.keys[debugStaticEpoch]
		for e := doPublishEpoch; e < doPublishEpoch+numMixKeys; e++ {
			desc.MixKeys[e] = staticKey.PublicKey()
		}
	} else if didGen, err := p.s.mixKeys.generateMixKeys(doPublishEpoch); err == nil {
		// Prune off the old mix keys.  Bad things happen if the epoch ever
		// goes backwards, but everyone uses NTP right?
		didPrune := p.s.mixKeys.pruneMixKeys()

		// Add the keys to the descriptor.
		for e := doPublishEpoch; e < doPublishEpoch+numMixKeys; e++ {
			// Why, yes, this doesn't hold the lock.  The only time the map is
			// altered is in mixkeys.generateMixKeys(), and mixkeys.pruneMixKeys(),
			// both of which are only called from this code path serially.
			k, ok := p.s.mixKeys.keys[e]
			if !ok {
				// The prune pass must have purged a key we intended to publish,
				// so bail out and try again in a little while.
				return fmt.Errorf("key that was scheduled for publication got pruned")
			}
			desc.MixKeys[e] = k.PublicKey()
		}
		if didGen || didPrune {
			// Kick the crypto workers into reshadowing the mix keys,
			// since there are either new keys, or less old keys.
			p.s.reshadowCryptoWorkers()
		}
	} else {
		// Sad panda, failed to generate the keys.
		return err
	}

	// Post the descriptor to all the authorities.
	err := p.impl.Post(pkiCtx, doPublishEpoch, p.s.identityKey, desc)
	if err == nil {
		p.log.Debugf("Posted descriptor for epoch: %v", doPublishEpoch)
		p.lastPublishedEpoch = doPublishEpoch
	}

	return err
}

func (p *pki) documentsToFetch() []uint64 {
	const nextFetchTill = 45 * time.Minute

	ret := make([]uint64, 0, 2)
	now, _, till := epochtime.Now()

	p.RLock()
	defer p.RUnlock()

	// Fetch the document for the current epoch if it is missing.
	if _, ok := p.docs[now]; !ok {
		ret = append(ret, now)
	}

	// If it is after the time that the next PKI has been generated, fetch
	// that as well, assuming it is missing.
	if till < nextFetchTill {
		if _, ok := p.docs[now+1]; !ok {
			ret = append(ret, now+1)
		}
	}

	return ret
}

func (p *pki) documentsForAuthentication() ([]*pkicache.Entry, uint64) {
	const (
		pkiEarlyConnectSlack = 30 * time.Minute
		pkiLateConnectSlack  = 3 * time.Minute
	)

	// Figure out the list of epochs to consider valid.
	now, elapsed, till := epochtime.Now()
	epochs := []uint64{now}
	if till < pkiEarlyConnectSlack {
		// Allow connections to new nodes 30 mins in advance of an epoch
		// transition.
		epochs = append(epochs, now+1)
	} else if elapsed < pkiLateConnectSlack {
		// Allow connections to old notes to linger for 3 mins past the epoch
		// transition.
		epochs = append(epochs, now-1)
	}

	// Return the list of cache entries.
	p.RLock()
	defer p.RUnlock()

	s := make([]*pkicache.Entry, 0, len(epochs))
	for _, epoch := range epochs {
		if e, ok := p.docs[epoch]; ok {
			s = append(s, e)
		}
	}
	return s, now
}

func (p *pki) authenticateIncoming(c *wire.PeerCredentials) (canSend, isValid bool) {
	const (
		earlySendSlack = 2 * time.Minute
		lateSendSlack  = 2 * time.Minute
	)

	// If mix authentication is disabled, then we just allow everyone to
	// connect as a mix.
	if p.s.cfg.Debug.DisableMixAuthentication {
		p.log.Debugf("Incoming: Blindly authenticating peer: '%v'(%v).", bytesToPrintString(c.AdditionalData), c.PublicKey)
		return true, true
	}

	if len(c.AdditionalData) != constants.NodeIDLength {
		p.log.Debugf("Incoming: '%v' AD not an IdentityKey?.", bytesToPrintString(c.AdditionalData))
		return false, false
	}
	var nodeID [constants.NodeIDLength]byte
	copy(nodeID[:], c.AdditionalData)

	docs, _ := p.documentsForAuthentication()
	now, elapsed, till := epochtime.Now()
	for _, d := range docs {
		desc := d.GetIncomingByID(nodeID)
		if desc == nil {
			continue
		}
		if !desc.LinkKey.Equal(c.PublicKey) {
			// The LinkKey that is being used for authentication should
			// match what is listed in the descriptor.
			p.log.Warningf("Incoming: '%v' Public Key mismatch: '%v'", bytesToPrintString(c.AdditionalData), c.PublicKey)
			continue
		}

		// The node is listed in a consensus that's reasonably current.
		isValid = true

		// Figure out if the node is allowed to send packets.
		switch d.Epoch() {
		case now:
			// The node is listed in the document for the current epoch.
			return true, true
		case now + 1:
			if till < earlySendSlack {
				// The node is listed in the document from the next epoch,
				// and it is less than slack till the transition.
				return true, true
			}
		case now - 1:
			if elapsed < lateSendSlack {
				// The node is listed in the document for the previous epoch,
				// and less than slack has past since the transition.
				return true, true
			}
		default:
		}

		// Well, this document doesn't seem to think that the node should
		// be able to send packets at us, maybe the other document if any
		// will be more forgiving.
	}

	return
}

func (p *pki) authenticateOutgoing(c *wire.PeerCredentials) (desc *cpki.MixDescriptor, canSend, isValid bool) {
	// If mix authentication is disabled, then we just blindly blast away.
	if p.s.cfg.Debug.DisableMixAuthentication {
		p.log.Debugf("Outgoing: Blindly authenticating peer: '%v'(%v).", bytesToPrintString(c.AdditionalData), c.PublicKey)
		return nil, true, true
	}

	// Don't need to check length here because all callers either explicitly
	// set this, or validate it (assuming it's coming from a peer).
	var nodeID [constants.NodeIDLength]byte
	copy(nodeID[:], c.AdditionalData)

	docs, now := p.documentsForAuthentication()
	for _, d := range docs {
		m := d.GetOutgoingByID(nodeID)
		if m == nil {
			continue
		}
		if !m.LinkKey.Equal(c.PublicKey) {
			// The LinkKey that is being used for authentication should
			// match what is listed in the descriptor.
			p.log.Warningf("Outgoing: '%v' Public Key mismatch: '%v'", bytesToPrintString(c.AdditionalData), c.PublicKey)
			continue
		}

		// The node is listed in a consensus that's reasonably current.
		isValid = true
		desc = m

		// If this is the document for the current epoch, the node is listed in
		// it, and we can send packets.
		//
		// Note: This is more strict than the incoming case since the main
		// reason the slack time exists is to account for clock skew, and it's
		// all handled there.
		if d.Epoch() == now {
			return m, true, true
		}

		// But we're not sure if we can send packets to it yet.
	}

	return
}

func (p *pki) outgoingDestinations() map[[constants.NodeIDLength]byte]*cpki.MixDescriptor {
	docs, _ := p.documentsForAuthentication()
	descMap := make(map[[constants.NodeIDLength]byte]*cpki.MixDescriptor)

	for _, d := range docs {
		for _, v := range d.Outgoing() {
			nodeID := v.IdentityKey.ByteArray()

			// De-duplicate.
			if _, ok := descMap[nodeID]; !ok {
				descMap[nodeID] = v
			}
		}
	}
	return descMap
}

func (p *pki) isValidForwardDest(id *[constants.NodeIDLength]byte) bool {
	// If mix authentication is disabled, then we just queue all the packets.
	if p.s.cfg.Debug.DisableMixAuthentication {
		return true
	}

	// This doesn't need to be super accurate, just enough to prevent packets
	// destined to la-la land from being scheduled.  As a mix we should
	// basically never see packets destined for nodes not listed in the
	// current consensus unless a node gets delisted.
	p.RLock()
	defer p.RUnlock()

	now, _, _ := epochtime.Now()
	doc, ok := p.docs[now]
	if !ok {
		return false
	}
	return doc.GetOutgoingByID(*id) != nil
}

func newPKI(s *Server) (*pki, error) {
	p := new(pki)
	p.s = s
	p.log = s.logBackend.GetLogger("pki")
	p.docs = make(map[uint64]*pkicache.Entry)

	if s.cfg.PKI.Nonvoting != nil {
		authPk := new(eddsa.PublicKey)
		err := authPk.FromString(s.cfg.PKI.Nonvoting.PublicKey)
		if err != nil {
			panic("BUG: Failed to deserialize validated public key: " + err.Error())
		}
		pkiCfg := &nClient.Config{
			LogBackend: s.logBackend,
			Address:    s.cfg.PKI.Nonvoting.Address,
			PublicKey:  authPk,
		}
		p.impl, err = nClient.New(pkiCfg)
		if err != nil {
			return nil, err
		}
	}
	// TODO: Wire in a real PKI implementation in addition to the test one.

	// Note: This does not start the worker immediately since the worker can
	// make calls into the connector and crypto workers (on PKI updates),
	// which are initialized after the pki object.

	return p, nil
}