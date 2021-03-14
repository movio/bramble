package bramble

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	log "github.com/sirupsen/logrus"
)

type PluginConfig struct {
	Name   string
	Config json.RawMessage
}

// Config contains the gateway configuration
type Config struct {
	GatewayPort            int       `json:"gateway-port"`
	MetricsPort            int       `json:"metrics-port"`
	PrivatePort            int       `json:"private-port"`
	Services               []string  `json:"services"`
	LogLevel               log.Level `json:"loglevel"`
	PollInterval           string    `json:"poll-interval"`
	PollIntervalDuration   time.Duration
	MaxRequestsPerQuery    int64 `json:"max-requests-per-query"`
	MaxServiceResponseSize int64 `json:"max-service-response-size"`
	Plugins                []PluginConfig
	// Config extensions that can be shared among plugins
	Extensions map[string]json.RawMessage

	plugins          []Plugin
	executableSchema *ExecutableSchema
	watcher          *fsnotify.Watcher
	configFiles      []string
	linkedFiles      []string
}

// GatewayAddress returns the host:port string of the gateway
func (c *Config) GatewayAddress() string {
	return fmt.Sprintf(":%d", c.GatewayPort)
}

func (c *Config) PrivateAddress() string {
	return fmt.Sprintf(":%d", c.PrivatePort)
}

func (c *Config) MetricAddress() string {
	return fmt.Sprintf(":%d", c.MetricsPort)
}

func (cfg *Config) Load() error {
	cfg.Extensions = nil
	// concatenate plugins from all the config files
	var plugins []PluginConfig
	for _, configFile := range cfg.configFiles {
		cfg.Plugins = nil
		f, err := os.Open(configFile)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := json.NewDecoder(f).Decode(&cfg); err != nil {
			return fmt.Errorf("error decoding config file %q: %w", configFile, err)
		}
		plugins = append(plugins, cfg.Plugins...)
	}
	cfg.Plugins = plugins

	logLevel := os.Getenv("BRAMBLE_LOG_LEVEL")
	if level, err := log.ParseLevel(logLevel); err == nil {
		cfg.LogLevel = level
	} else if logLevel != "" {
		log.WithField("loglevel", logLevel).Warn("invalid loglevel")
	}
	log.SetLevel(cfg.LogLevel)

	var err error
	cfg.PollIntervalDuration, err = time.ParseDuration(cfg.PollInterval)
	if err != nil {
		return fmt.Errorf("invalid poll interval: %w", err)
	}

	services, err := cfg.buildServiceList()
	if err != nil {
		return err
	}
	cfg.Services = services

	cfg.plugins = cfg.ConfigurePlugins()

	return nil
}

func (cfg *Config) buildServiceList() ([]string, error) {
	serviceSet := map[string]bool{}
	for _, service := range cfg.Services {
		serviceSet[service] = true
	}
	for _, service := range strings.Fields(os.Getenv("BRAMBLE_SERVICE_LIST")) {
		serviceSet[service] = true
	}
	for _, plugin := range cfg.plugins {
		ok, path := plugin.GraphqlQueryPath()
		if ok {
			service := fmt.Sprintf("http://localhost:%d/%s", cfg.PrivatePort, path)
			serviceSet[service] = true
		}
	}
	services := []string{}
	for service := range serviceSet {
		services = append(services, service)
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("no services found in BRAMBLE_SERVICE_LIST or %s", cfg.configFiles)
	}
	return services, nil
}

func (cfg *Config) Watch() {
	for {
		select {
		case err := <-cfg.watcher.Errors:
			log.WithError(err).Error("config watch error")
		case e := <-cfg.watcher.Events:
			log.WithFields(log.Fields{"event": e, "files": cfg.configFiles, "links": cfg.linkedFiles}).Debug("received config file event")
			shouldUpdate := false
			for i := range cfg.configFiles {
				// we want to reload the config if:
				// - the config file was updated, or
				// - the config file is a symlink and was changed (k8s config map update)
				if filepath.Clean(e.Name) == cfg.configFiles[i] && (e.Op == fsnotify.Write || e.Op == fsnotify.Create) {
					shouldUpdate = true
					break
				}
				currentFile, _ := filepath.EvalSymlinks(cfg.configFiles[i])
				if cfg.linkedFiles[i] != "" && cfg.linkedFiles[i] != currentFile {
					cfg.linkedFiles[i] = currentFile
					shouldUpdate = true
					break
				}
			}

			if !shouldUpdate {
				log.Debug("nothing to update")
				continue
			}

			if e.Op != fsnotify.Write && e.Op != fsnotify.Create {
				log.Debug("ignoring non write/create event")
				continue
			}

			err := cfg.Load()
			if err != nil {
				log.WithError(err).Error("error reloading config")
			}
			log.WithField("services", cfg.Services).Info("config file updated")
			err = cfg.executableSchema.UpdateServiceList(cfg.Services)
			if err != nil {
				log.WithError(err).Error("error updating services")
			}
			log.WithField("services", cfg.Services).Info("updated services")
		}
	}
}

// GetConfig returns operational config for the gateway
func GetConfig(configFiles []string) (*Config, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("could not create watcher: %w", err)
	}
	var linkedFiles []string
	for _, configFile := range configFiles {
		// watch the directory, else we'll lose the watch if the file is relinked
		err = watcher.Add(filepath.Dir(configFile))
		if err != nil {
			return nil, fmt.Errorf("error add file to watcher: %w", err)
		}
		linkedFile, _ := filepath.EvalSymlinks(configFile)
		linkedFiles = append(linkedFiles, linkedFile)
	}

	cfg := Config{
		GatewayPort:            8082,
		PrivatePort:            8083,
		MetricsPort:            9009,
		LogLevel:               log.DebugLevel,
		PollInterval:           "5s",
		MaxRequestsPerQuery:    50,
		MaxServiceResponseSize: 1024 * 1024,

		watcher:     watcher,
		configFiles: configFiles,
		linkedFiles: linkedFiles,
	}
	err = cfg.Load()

	return &cfg, err
}

func (c *Config) ConfigurePlugins() []Plugin {
	var enabledPlugins []Plugin
	for _, pl := range c.Plugins {
		p, ok := RegisteredPlugins()[pl.Name]
		if !ok {
			log.Warnf("plugin %q not found", pl.Name)
			continue
		}
		err := p.Configure(c, pl.Config)
		if err != nil {
			log.WithError(err).Fatalf("error unmarshalling config for plugin %q: %s", pl.Name, err)
		}
		enabledPlugins = append(enabledPlugins, p)
	}

	return enabledPlugins
}

func (c *Config) Init() error {
	var err error
	c.Services, err = c.buildServiceList()
	if err != nil {
		return fmt.Errorf("error building service list: %w", err)
	}

	var services []*Service
	for _, s := range c.Services {
		services = append(services, NewService(s))
	}

	client := NewClient(WithMaxResponseSize(c.MaxRequestsPerQuery))
	es := newExecutableSchema(c.plugins, c.MaxRequestsPerQuery, client, services...)
	err = es.UpdateSchema(true)
	if err != nil {
		return err
	}

	c.executableSchema = es

	var pluginsNames []string
	for _, plugin := range c.plugins {
		plugin.Init(c.executableSchema)
		pluginsNames = append(pluginsNames, plugin.ID())
	}
	log.Infof("enabled plugins: %v", pluginsNames)

	return nil
}

type arrayFlags []string

func (a *arrayFlags) String() string {
	return strings.Join(*a, ",")
}

func (a *arrayFlags) Set(value string) error {
	*a = append(*a, value)
	return nil
}
