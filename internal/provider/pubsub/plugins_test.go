// plugins_test.go - tests for plugin system
// Copyright (C) 2020  David Stainton
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

package pubsub

import (
	"testing"

	"github.com/katzenpost/core/crypto/ecdh"
	"github.com/katzenpost/core/crypto/eddsa"
	"github.com/katzenpost/core/crypto/rand"
	"github.com/katzenpost/core/log"
	"github.com/katzenpost/core/sphinx/constants"
	"github.com/katzenpost/core/thwack"
	"github.com/katzenpost/core/wire"
	"github.com/katzenpost/server/config"
	"github.com/katzenpost/server/internal/glue"
	"github.com/katzenpost/server/internal/packet"
	"github.com/katzenpost/server/internal/pkicache"
	"github.com/katzenpost/server/spool"
	"github.com/katzenpost/server/userdb"
	"github.com/stretchr/testify/require"
)

type mockUserDB struct {
	provider *mockProvider
}

func (u *mockUserDB) Exists([]byte) bool {
	return true
}

func (u *mockUserDB) IsValid([]byte, *ecdh.PublicKey) bool { return true }

func (u *mockUserDB) Add([]byte, *ecdh.PublicKey, bool) error { return nil }

func (u *mockUserDB) SetIdentity([]byte, *ecdh.PublicKey) error { return nil }

func (u *mockUserDB) Link([]byte) (*ecdh.PublicKey, error) {
	return nil, nil
}

func (u *mockUserDB) Identity([]byte) (*ecdh.PublicKey, error) {
	return u.provider.userKey, nil
}

func (u *mockUserDB) Remove([]byte) error { return nil }

func (u *mockUserDB) Close() {}

type mockSpool struct{}

func (s *mockSpool) StoreMessage(u, msg []byte) error { return nil }

func (s *mockSpool) StoreSURBReply(u []byte, id *[constants.SURBIDLength]byte, msg []byte) error {
	return nil
}

func (s *mockSpool) Get(u []byte, advance bool) (msg, surbID []byte, remaining int, err error) {
	return []byte{1, 2, 3}, nil, 1, nil
}

func (s *mockSpool) Remove(u []byte) error { return nil }

func (s *mockSpool) Vacuum(udb userdb.UserDB) error { return nil }

func (s *mockSpool) Close() {}

type mockProvider struct {
	userName string
	userKey  *ecdh.PublicKey
}

func (p *mockProvider) Halt() {}

func (p *mockProvider) UserDB() userdb.UserDB {
	return &mockUserDB{
		provider: p,
	}
}

func (p *mockProvider) Spool() spool.Spool {
	return &mockSpool{}
}

func (p *mockProvider) AuthenticateClient(*wire.PeerCredentials) bool {
	return true
}

func (p *mockProvider) OnPacket(*packet.Packet) {}

func (p *mockProvider) KaetzchenForPKI() (map[string]map[string]interface{}, error) {
	return nil, nil
}

func (p *mockProvider) AdvertiseRegistrationHTTPAddresses() []string {
	return nil
}

type mockDecoy struct{}

func (d *mockDecoy) Halt() {}

func (d *mockDecoy) OnNewDocument(*pkicache.Entry) {}

func (d *mockDecoy) OnPacket(*packet.Packet) {}

type mockServer struct {
	cfg         *config.Config
	logBackend  *log.Backend
	identityKey *eddsa.PrivateKey
	linkKey     *ecdh.PrivateKey
	management  *thwack.Server
	mixKeys     glue.MixKeys
	pki         glue.PKI
	provider    glue.Provider
	scheduler   glue.Scheduler
	connector   glue.Connector
	listeners   []glue.Listener
}

type mockGlue struct {
	s *mockServer
}

func (g *mockGlue) Config() *config.Config {
	return g.s.cfg
}

func (g *mockGlue) LogBackend() *log.Backend {
	return g.s.logBackend
}

func (g *mockGlue) IdentityKey() *eddsa.PrivateKey {
	return g.s.identityKey
}

func (g *mockGlue) LinkKey() *ecdh.PrivateKey {
	return g.s.linkKey
}

func (g *mockGlue) Management() *thwack.Server {
	return g.s.management
}

func (g *mockGlue) MixKeys() glue.MixKeys {
	return g.s.mixKeys
}

func (g *mockGlue) PKI() glue.PKI {
	return g.s.pki
}

func (g *mockGlue) Provider() glue.Provider {
	return g.s.provider
}

func (g *mockGlue) Scheduler() glue.Scheduler {
	return g.s.scheduler
}

func (g *mockGlue) Connector() glue.Connector {
	return g.s.connector
}

func (g *mockGlue) Listeners() []glue.Listener {
	return g.s.listeners
}

func (g *mockGlue) ReshadowCryptoWorkers() {}

func (g *mockGlue) Decoy() glue.Decoy {
	return &mockDecoy{}
}

func getGlue(logBackend *log.Backend, provider *mockProvider, linkKey *ecdh.PrivateKey, idKey *eddsa.PrivateKey) *mockGlue {
	goo := &mockGlue{
		s: &mockServer{
			logBackend: logBackend,
			provider:   provider,
			linkKey:    linkKey,
			cfg: &config.Config{
				Server:     &config.Server{},
				Logging:    &config.Logging{},
				Provider:   &config.Provider{},
				PKI:        &config.PKI{},
				Management: &config.Management{},
				Debug: &config.Debug{
					NumKaetzchenWorkers: 3,
					IdentityKey:         idKey,
					KaetzchenDelay:      300,
				},
			},
		},
	}
	return goo
}

func TestPubsubInvalidCommand(t *testing.T) {
	require := require.New(t)

	idKey, err := eddsa.NewKeypair(rand.Reader)
	require.NoError(err)

	logBackend, err := log.New("", "DEBUG", false)
	require.NoError(err)

	userKey, err := ecdh.NewKeypair(rand.Reader)
	require.NoError(err)

	linkKey, err := ecdh.NewKeypair(rand.Reader)
	require.NoError(err)

	mockProvider := &mockProvider{
		userName: "alice",
		userKey:  userKey.PublicKey(),
	}

	goo := getGlue(logBackend, mockProvider, linkKey, idKey)
	goo.s.cfg.Provider.PubsubPlugin = []*config.PubsubPlugin{
		&config.PubsubPlugin{
			Capability:     "loop",
			Endpoint:       "loop",
			Config:         map[string]interface{}{},
			Disable:        false,
			Command:        "non-existent command",
			MaxConcurrency: 1,
		},
	}
	_, err = New(goo)
	require.Error(err)
}
