// Package plugin registers the WebDAV datastore with kubo.
//
// It is the only package that imports kubo, keeping the engine
// (github.com/ulbwa/kubo-ds-webdav) free of the kubo dependency.
package plugin

import (
	"fmt"
	"time"

	webdavds "github.com/ulbwa/kubo-ds-webdav"

	"github.com/ipfs/kubo/plugin"
	"github.com/ipfs/kubo/repo"
	"github.com/ipfs/kubo/repo/fsrepo"
)

// Plugins is the exported symbol kubo's loader looks up.
var Plugins = []plugin.Plugin{
	&WebDAVPlugin{},
}

// WebDAVPlugin implements plugin.PluginDatastore.
type WebDAVPlugin struct{}

func (WebDAVPlugin) Name() string                       { return "webdav-datastore-plugin" }
func (WebDAVPlugin) Version() string                    { return "0.1.0" }
func (WebDAVPlugin) Init(_ *plugin.Environment) error   { return nil }
func (WebDAVPlugin) DatastoreTypeName() string          { return "webdavds" }

// DatastoreConfigParser parses the "webdavds" child of the kubo datastore spec.
func (WebDAVPlugin) DatastoreConfigParser() fsrepo.ConfigFromMap {
	return func(m map[string]interface{}) (fsrepo.DatastoreConfig, error) {
		url, ok := m["url"].(string)
		if !ok || url == "" {
			return nil, fmt.Errorf("webdavds: no url specified")
		}
		cfg := webdavds.Config{URL: url}

		if v, ok := m["rootDirectory"]; ok {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("webdavds: rootDirectory must be a string")
			}
			cfg.RootDirectory = s
		}
		if v, ok := m["username"]; ok {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("webdavds: username must be a string")
			}
			cfg.Username = s
		}
		if v, ok := m["password"]; ok {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("webdavds: password must be a string")
			}
			cfg.Password = s
		}
		if v, ok := m["shardFunc"]; ok {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("webdavds: shardFunc must be a string")
			}
			cfg.ShardFunc = s
		}
		if v, ok := m["concurrency"]; ok {
			f, ok := v.(float64)
			if !ok || f <= 0 || float64(int(f)) != f {
				return nil, fmt.Errorf("webdavds: concurrency must be a positive integer")
			}
			cfg.Concurrency = int(f)
		}
		if v, ok := m["noDelete"]; ok {
			b, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("webdavds: noDelete must be a boolean")
			}
			cfg.NoDelete = b
		}
		if v, ok := m["useMove"]; ok {
			b, ok := v.(bool)
			if !ok {
				return nil, fmt.Errorf("webdavds: useMove must be a boolean")
			}
			cfg.UseMove = &b
		}
		if v, ok := m["connTimeout"]; ok {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("webdavds: connTimeout must be a duration string")
			}
			d, err := time.ParseDuration(s)
			if err != nil {
				return nil, fmt.Errorf("webdavds: invalid connTimeout: %w", err)
			}
			cfg.ConnTimeout = d
		}
		if v, ok := m["requestTimeout"]; ok {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("webdavds: requestTimeout must be a duration string")
			}
			d, err := time.ParseDuration(s)
			if err != nil {
				return nil, fmt.Errorf("webdavds: invalid requestTimeout: %w", err)
			}
			cfg.RequestTimeout = d
		}
		if v, ok := m["headers"]; ok {
			hm, ok := v.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("webdavds: headers must be an object")
			}
			cfg.Headers = make(map[string]string, len(hm))
			for k, hv := range hm {
				s, ok := hv.(string)
				if !ok {
					return nil, fmt.Errorf("webdavds: header %q must be a string", k)
				}
				cfg.Headers[k] = s
			}
		}

		return &WebDAVConfig{cfg: cfg}, nil
	}
}

// WebDAVConfig implements fsrepo.DatastoreConfig.
type WebDAVConfig struct {
	cfg webdavds.Config
}

// DiskSpec is the datastore fingerprint kubo compares on every start. It holds
// only the identity of the backing store (NOT credentials or tunables), so
// rotating credentials never triggers a spurious spec-mismatch migration.
func (c *WebDAVConfig) DiskSpec() fsrepo.DiskSpec {
	return fsrepo.DiskSpec{
		"type":          "webdavds",
		"url":           c.cfg.URL,
		"rootDirectory": c.cfg.RootDirectory,
	}
}

// Create opens the datastore. The local `path` (the kubo repo dir) is unused:
// all data lives in WebDAV.
func (c *WebDAVConfig) Create(_ string) (repo.Datastore, error) {
	return webdavds.New(c.cfg)
}
