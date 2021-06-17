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

var Version = "dev"

// PluginConfig contains the configuration for the named plugin
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

// PrivateAddress returns the address for private port
func (c *Config) PrivateAddress() string {
	return fmt.Sprintf(":%d", c.PrivatePort)
}

// MetricAddress returns the address for the metric port
func (c *Config) MetricAddress() string {
	return fmt.Sprintf(":%d", c.MetricsPort)
}

// Load loads or reloads all the config files.
func (c *Config) Load() error {
	c.Extensions = nil
	// concatenate plugins from all the config files
	var plugins []PluginConfig
	for _, configFile := range c.configFiles {
		c.Plugins = nil
		f, err := os.Open(configFile)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := json.NewDecoder(f).Decode(&c); err != nil {
			return fmt.Errorf("error decoding config file %q: %w", configFile, err)
		}
		plugins = append(plugins, c.Plugins...)
	}
	c.Plugins = plugins

	logLevel := os.Getenv("BRAMBLE_LOG_LEVEL")
	if level, err := log.ParseLevel(logLevel); err == nil {
		c.LogLevel = level
	} else if logLevel != "" {
		log.WithField("loglevel", logLevel).Warn("invalid loglevel")
	}
	log.SetLevel(c.LogLevel)

	var err error
	c.PollIntervalDuration, err = time.ParseDuration(c.PollInterval)
	if err != nil {
		return fmt.Errorf("invalid poll interval: %w", err)
	}

	services, err := c.buildServiceList()
	if err != nil {
		return err
	}
	c.Services = services

	c.plugins = c.ConfigurePlugins()

	return nil
}

func (c *Config) buildServiceList() ([]string, error) {
	serviceSet := map[string]bool{}
	for _, service := range c.Services {
		serviceSet[service] = true
	}
	for _, service := range strings.Fields(os.Getenv("BRAMBLE_SERVICE_LIST")) {
		serviceSet[service] = true
	}
	for _, plugin := range c.plugins {
		ok, path := plugin.GraphqlQueryPath()
		if ok {
			service := fmt.Sprintf("http://localhost:%d/%s", c.PrivatePort, path)
			serviceSet[service] = true
		}
	}
	services := []string{}
	for service := range serviceSet {
		services = append(services, service)
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("no services found in BRAMBLE_SERVICE_LIST or %s", c.configFiles)
	}
	return services, nil
}

// Watch starts watching the config files for change.
func (c *Config) Watch() {
	for {
		select {
		case err := <-c.watcher.Errors:
			log.WithError(err).Error("config watch error")
		case e := <-c.watcher.Events:
			log.WithFields(log.Fields{"event": e, "files": c.configFiles, "links": c.linkedFiles}).Debug("received config file event")
			shouldUpdate := false
			for i := range c.configFiles {
				// we want to reload the config if:
				// - the config file was updated, or
				// - the config file is a symlink and was changed (k8s config map update)
				if filepath.Clean(e.Name) == c.configFiles[i] && (e.Op == fsnotify.Write || e.Op == fsnotify.Create) {
					shouldUpdate = true
					break
				}
				currentFile, _ := filepath.EvalSymlinks(c.configFiles[i])
				if c.linkedFiles[i] != "" && c.linkedFiles[i] != currentFile {
					c.linkedFiles[i] = currentFile
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

			err := c.Load()
			if err != nil {
				log.WithError(err).Error("error reloading config")
			}
			log.WithField("services", c.Services).Info("config file updated")
			err = c.executableSchema.UpdateServiceList(c.Services)
			if err != nil {
				log.WithError(err).Error("error updating services")
			}
			log.WithField("services", c.Services).Info("updated services")
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

// ConfigurePlugins calls the Configure method on each plugin.
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

// Init initializes the config and does an initial fetch of the services.
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

	queryClient := NewClient(WithMaxResponseSize(c.MaxServiceResponseSize), WithUserAgent(GenerateUserAgent("query")))
	es := newExecutableSchema(c.plugins, c.MaxRequestsPerQuery, queryClient, services...)
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
