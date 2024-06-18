package bramble

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

var Version = "dev"

// PluginConfig contains the configuration for the named plugin
type PluginConfig struct {
	Name   string
	Config json.RawMessage
}

type TimeoutConfig struct {
	ReadTimeout          string        `json:"read"`
	ReadTimeoutDuration  time.Duration `json:"-"`
	WriteTimeout         string        `json:"write"`
	WriteTimeoutDuration time.Duration `json:"-"`
	IdleTimeout          string        `json:"idle"`
	IdleTimeoutDuration  time.Duration `json:"-"`
}

// Config contains the gateway configuration
type Config struct {
	IdFieldName            string        `json:"id-field-name"`
	GatewayListenAddress   string        `json:"gateway-address"`
	DisableIntrospection   bool          `json:"disable-introspection"`
	MetricsListenAddress   string        `json:"metrics-address"`
	PrivateListenAddress   string        `json:"private-address"`
	GatewayPort            int           `json:"gateway-port"`
	MetricsPort            int           `json:"metrics-port"`
	PrivatePort            int           `json:"private-port"`
	DefaultTimeouts        TimeoutConfig `json:"default-timeouts"`
	GatewayTimeouts        TimeoutConfig `json:"gateway-timeouts"`
	PrivateTimeouts        TimeoutConfig `json:"private-timeouts"`
	Services               []string      `json:"services"`
	LogLevel               log.Level     `json:"loglevel"`
	PollInterval           string        `json:"poll-interval"`
	PollIntervalDuration   time.Duration
	MaxRequestsPerQuery    int64           `json:"max-requests-per-query"`
	MaxServiceResponseSize int64           `json:"max-service-response-size"`
	MaxFileUploadSize      int64           `json:"max-file-upload-size"`
	Telemetry              TelemetryConfig `json:"telemetry"`
	Plugins                []PluginConfig
	// Config extensions that can be shared among plugins
	Extensions map[string]json.RawMessage
	// HTTP client to customize for downstream services query
	QueryHTTPClient *http.Client

	plugins          []Plugin
	executableSchema *ExecutableSchema
	watcher          *fsnotify.Watcher
	tracer           trace.Tracer
	configFiles      []string
	linkedFiles      []string
}

func (c *Config) addrOrPort(addr string, port int) string {
	if addr != "" {
		return addr
	}
	return fmt.Sprintf(":%d", port)
}

// GatewayAddress returns the host:port string of the gateway
func (c *Config) GatewayAddress() string {
	return c.addrOrPort(c.GatewayListenAddress, c.GatewayPort)
}

// PrivateAddress returns the address for private port
func (c *Config) PrivateAddress() string {
	return c.addrOrPort(c.PrivateListenAddress, c.PrivatePort)
}

func (c *Config) PrivateHttpAddress(path string) string {
	if c.PrivateListenAddress == "" {
		return fmt.Sprintf("http://localhost:%d/%s", c.PrivatePort, path)
	}
	return fmt.Sprintf("http://%s/%s", c.PrivateListenAddress, path)
}

// MetricAddress returns the address for the metric port
func (c *Config) MetricAddress() string {
	return c.addrOrPort(c.MetricsListenAddress, c.MetricsPort)
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

	if strings.TrimSpace(c.IdFieldName) != "" {
		IdFieldName = c.IdFieldName
	}

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

	c.DefaultTimeouts.ReadTimeoutDuration, err = time.ParseDuration(c.DefaultTimeouts.ReadTimeout)
	if err != nil {
		return fmt.Errorf("invalid default read timeout: %w", err)
	}
	c.DefaultTimeouts.WriteTimeoutDuration, err = time.ParseDuration(c.DefaultTimeouts.WriteTimeout)
	if err != nil {
		return fmt.Errorf("invalid default write timeout: %w", err)
	}
	c.DefaultTimeouts.IdleTimeoutDuration, err = time.ParseDuration(c.DefaultTimeouts.IdleTimeout)
	if err != nil {
		return fmt.Errorf("invalid default idle timeout: %w", err)
	}
	if err = c.loadTimeouts(&c.GatewayTimeouts, "gateway", c.DefaultTimeouts); err != nil {
		return err
	}
	if err = c.loadTimeouts(&c.PrivateTimeouts, "private", c.DefaultTimeouts); err != nil {
		return err
	}

	services, err := c.buildServiceList()
	if err != nil {
		return err
	}
	c.Services = services

	c.plugins = c.ConfigurePlugins()

	return nil
}

func (c *Config) loadTimeouts(config *TimeoutConfig, name string, defaults TimeoutConfig) error {
	var err error
	if config.ReadTimeout != "" {
		config.ReadTimeoutDuration, err = time.ParseDuration(config.ReadTimeout)
		if err != nil {
			return fmt.Errorf("invalid %s read timeout: %w", name, err)
		}
	}
	if config.ReadTimeoutDuration == 0 {
		config.ReadTimeoutDuration = defaults.ReadTimeoutDuration
	}
	if config.WriteTimeout != "" {
		config.WriteTimeoutDuration, err = time.ParseDuration(config.WriteTimeout)
		if err != nil {
			return fmt.Errorf("invalid %s write timeout: %w", name, err)
		}
	}
	if config.WriteTimeoutDuration == 0 {
		config.WriteTimeoutDuration = defaults.WriteTimeoutDuration
	}
	if config.IdleTimeout != "" {
		config.IdleTimeoutDuration, err = time.ParseDuration(config.IdleTimeout)
		if err != nil {
			return fmt.Errorf("invalid %s idle timeout: %w", name, err)
		}
	}
	if config.IdleTimeoutDuration == 0 {
		config.IdleTimeoutDuration = defaults.IdleTimeoutDuration
	}
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
			service := c.PrivateHttpAddress(path)
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

			if err := c.reload(); err != nil {
				log.WithError(err).Error("error reloading config")
			}
		}
	}
}

func (c *Config) reload() error {
	ctx := context.Background()

	ctx, span := c.tracer.Start(ctx, "Config Reload")
	defer span.End()

	if err := c.Load(); err != nil {
		log.WithError(err).Error("error reloading config")
	}

	log.WithField("services", c.Services).Info("config file updated")

	if err := c.executableSchema.UpdateServiceList(ctx, c.Services); err != nil {
		log.WithError(err).Error("error updating services")
	}

	log.WithField("services", c.Services).Info("updated services")

	return nil
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
		DefaultTimeouts: TimeoutConfig{
			ReadTimeout:  "5s",
			WriteTimeout: "10s",
			IdleTimeout:  "120s",
		},
		GatewayPort:            8082,
		PrivatePort:            8083,
		MetricsPort:            9009,
		LogLevel:               log.DebugLevel,
		PollInterval:           "10s",
		MaxRequestsPerQuery:    50,
		MaxServiceResponseSize: 1024 * 1024,

		watcher:     watcher,
		tracer:      otel.GetTracerProvider().Tracer(instrumentationName),
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

	serviceClientOptions := []ClientOpt{
		WithMaxResponseSize(c.MaxServiceResponseSize),
	}
	if c.QueryHTTPClient != nil {
		serviceClientOptions = append(serviceClientOptions, WithHTTPClient(c.QueryHTTPClient))
	}

	var services []*Service
	for _, s := range c.Services {
		services = append(services, NewService(s, serviceClientOptions...))
	}

	queryClientOptions := []ClientOpt{
		WithMaxResponseSize(c.MaxServiceResponseSize),
		WithUserAgent(GenerateUserAgent("query")),
	}

	if c.QueryHTTPClient != nil {
		queryClientOptions = append(queryClientOptions, WithHTTPClient(c.QueryHTTPClient))
	}
	queryClient := NewClientWithPlugins(c.plugins, queryClientOptions...)
	es := NewExecutableSchema(c.plugins, c.MaxRequestsPerQuery, queryClient, services...)
	err = es.UpdateSchema(context.Background(), true)
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
