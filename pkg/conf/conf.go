/*
 * Copyright NetApp Inc, 2021 All rights reserved
 */

package conf

import (
	"fmt"
	"github.com/imdario/mergo"
	"github.com/netapp/harvest/v2/pkg/errs"
	"github.com/netapp/harvest/v2/pkg/tree/node"
	"github.com/netapp/harvest/v2/pkg/util"
	"gopkg.in/yaml.v3"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var Config = HarvestConfig{}
var configRead = false

const (
	DefaultAPIVersion = "1.3"
	DefaultTimeout    = "30s"
	HarvestYML        = "harvest.yml"
	BasicAuth         = "basic_auth"
	CertificateAuth   = "certificate_auth"
)

// TestLoadHarvestConfig is used by testing code to reload a new config
func TestLoadHarvestConfig(configPath string) {
	configRead = false
	promPortRangeMapping = make(map[string]PortMap)
	err := LoadHarvestConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config at=[%s] err=%+v\n", configPath, err)
	}
}

func ConfigPath(path string) string {
	// Harvest uses the following precedence order. Each item takes precedence over the
	// item below it:
	// 1. --config command line flag
	// 2. HARVEST_CONFIG environment variable
	// 3. no command line argument and no environment variable, use the default path (HarvestYML)
	if path != HarvestYML && path != "./"+HarvestYML {
		return path
	}
	fp := os.Getenv("HARVEST_CONFIG")
	if fp == "" {
		return path
	}
	return fp
}

func LoadHarvestConfig(configPath string) error {
	if configRead {
		return nil
	}
	configPath = ConfigPath(configPath)
	contents, err := os.ReadFile(configPath)

	if err != nil {
		fmt.Printf("error reading config file=[%s] %+v\n", configPath, err)
		return err
	}
	err = DecodeConfig(contents)
	if err != nil {
		fmt.Printf("error unmarshalling config file=[%s] %+v\n", configPath, err)
		return err
	}
	return nil
}

func DecodeConfig(contents []byte) error {
	err := yaml.Unmarshal(contents, &Config)
	configRead = true
	if err != nil {
		return fmt.Errorf("error unmarshalling config err: %w", err)
	}
	// Until https://github.com/go-yaml/yaml/issues/717 is fixed
	// read the yaml again to determine poller order
	orderedConfig := OrderedConfig{}
	err = yaml.Unmarshal(contents, &orderedConfig)
	if err != nil {
		return err
	}
	Config.PollersOrdered = orderedConfig.Pollers.namesInOrder
	for i, name := range Config.PollersOrdered {
		Config.Pollers[name].promIndex = i
	}

	// Merge pollers and defaults
	pollers := Config.Pollers
	defaults := Config.Defaults

	if pollers == nil {
		return errs.New(errs.ErrConfig, "[Pollers] section not found")
	}
	if defaults != nil {
		for _, p := range pollers {
			p.Union(defaults)
		}
	}
	return nil
}

func ReadCredentialFile(credPath string, p *Poller) error {
	contents, err := os.ReadFile(credPath)
	if err != nil {
		abs, err2 := filepath.Abs(credPath)
		if err2 != nil {
			abs = credPath
		}
		return fmt.Errorf("failed to read file=%s error: %w", abs, err)
	}
	if p == nil {
		return nil
	}
	var credConfig HarvestConfig
	err = yaml.Unmarshal(contents, &credConfig)
	if err != nil {
		return err
	}

	credPoller := credConfig.Pollers[p.Name]
	if credPoller == nil {
		// when the poller is not listed in the file, check if there is a default, and if so, use it
		if credConfig.Defaults != nil {
			credPoller = credConfig.Defaults
		} else {
			return errs.New(errs.ErrInvalidParam, "poller not found in credentials file")
		}
	}
	if credPoller.SslKey != "" {
		p.SslKey = credPoller.SslKey
	}
	if credPoller.SslCert != "" {
		p.SslCert = credPoller.SslCert
	}
	if credPoller.CaCertPath != "" {
		p.CaCertPath = credPoller.CaCertPath
	}
	if credPoller.Username != "" {
		p.Username = credPoller.Username
	}
	if credPoller.Password != "" {
		p.Password = credPoller.Password
	}
	return nil
}

func PollerNamed(name string) (*Poller, error) {
	poller, ok := Config.Pollers[name]
	if !ok {
		return nil, errs.New(errs.ErrConfig, "poller ["+name+"] not found")
	}
	poller.Name = name
	return poller, nil
}

// GetDefaultHarvestConfigPath returns the absolute path of the default harvest config file.
func GetDefaultHarvestConfigPath() string {
	configPath := os.Getenv("HARVEST_CONF")
	if configPath == "" {
		return "./" + HarvestYML
	} else {
		return path.Join(configPath, HarvestYML)
	}
}

// GetHarvestHomePath returns the value of the env var HARVEST_CONF or ./
func GetHarvestHomePath() string {
	harvestConf := os.Getenv("HARVEST_CONF")
	if harvestConf == "" {
		return "./"
	}
	if strings.HasSuffix(harvestConf, "/") {
		return harvestConf
	}
	return harvestConf + "/"
}

func GetHarvestLogPath() string {
	logPath := os.Getenv("HARVEST_LOGS")
	if logPath == "" {
		return "/var/log/harvest/"
	}
	return logPath
}

// GetPrometheusExporterPorts returns the Prometheus port for the given poller
func GetPrometheusExporterPorts(pollerName string, validatePortInUse bool) (int, error) {
	var port int
	var isPrometheusExporterConfigured bool

	if len(promPortRangeMapping) == 0 {
		loadPrometheusExporterPortRangeMapping(validatePortInUse)
	}
	poller := Config.Pollers[pollerName]
	if poller == nil {
		return 0, errs.New(errs.ErrConfig, "Poller does not exist "+pollerName)
	}

	exporters := poller.Exporters
	if len(exporters) > 0 {
		for _, e := range exporters {
			exporter := Config.Exporters[e]
			if exporter.Type == "Prometheus" {
				isPrometheusExporterConfigured = true
				if exporter.PortRange != nil {
					ports := promPortRangeMapping[e]
					preferredPort := exporter.PortRange.Min + poller.promIndex
					_, isFree := ports.freePorts[preferredPort]
					if isFree {
						port = preferredPort
						delete(ports.freePorts, preferredPort)
						break
					}
					for k := range ports.freePorts {
						port = k
						delete(ports.freePorts, k)
						break
					}
				} else if exporter.Port != nil && *exporter.Port != 0 {
					port = *exporter.Port
					break
				}
			}
		}
	}
	if port == 0 && isPrometheusExporterConfigured {
		return port, errs.New(errs.ErrConfig, "No free port found for poller "+pollerName)
	}
	return port, nil
}

type PortMap struct {
	portSet   []int
	freePorts map[int]struct{}
}

func PortMapFromRange(address string, portRange *IntRange, validatePortInUse bool) PortMap {
	portMap := PortMap{}
	portMap.freePorts = make(map[int]struct{})
	start := portRange.Min
	end := portRange.Max
	for i := start; i <= end; i++ {
		portMap.portSet = append(portMap.portSet, i)
		if validatePortInUse {
			portMap.freePorts[i] = struct{}{}
		}
	}
	if !validatePortInUse {
		portMap.freePorts = util.CheckFreePorts(address, portMap.portSet)
	}
	return portMap
}

var promPortRangeMapping = make(map[string]PortMap)

func loadPrometheusExporterPortRangeMapping(validatePortInUse bool) {
	for k, v := range Config.Exporters {
		if v.Type == "Prometheus" {
			if v.PortRange != nil {
				// we only care about free ports on the localhost
				promPortRangeMapping[k] = PortMapFromRange("localhost", v.PortRange, validatePortInUse)
			}
		}
	}
}

type IntRange struct {
	Min int
	Max int
}

var rangeRegex, _ = regexp.Compile(`(\d+)\s*-\s*(\d+)`)

func (i *IntRange) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode && node.ShortTag() == "!!str" {
		matches := rangeRegex.FindStringSubmatch(node.Value)
		if len(matches) == 3 {
			min, err1 := strconv.Atoi(matches[1])
			max, err2 := strconv.Atoi(matches[2])
			if err1 != nil {
				return err1
			}
			if err2 != nil {
				return err2
			}
			i.Min = min
			i.Max = max
		}
	}
	return nil
}

// GetUniqueExporters returns the unique set of exporter types from the list of export names
// For example: If 2 prometheus exporters are configured for a poller, the last one is returned
func GetUniqueExporters(exporterNames []string) []string {
	var resultExporters []string
	definedExporters := Config.Exporters
	exporterMap := make(map[string]string)
	for _, ec := range exporterNames {
		e, ok := definedExporters[ec]
		if ok {
			exporterMap[e.Type] = ec
		}
	}

	for _, value := range exporterMap {
		resultExporters = append(resultExporters, value)
	}
	return resultExporters
}

type TLS struct {
	CertFile string `yaml:"cert_file,omitempty"`
	KeyFile  string `yaml:"key_file,omitempty"`
}

type Httpsd struct {
	Listen    string `yaml:"listen,omitempty"`
	AuthBasic struct {
		Username string `yaml:"username,omitempty"`
		Password string `yaml:"password,omitempty"`
	} `yaml:"auth_basic,omitempty"`
	TLS         TLS    `yaml:"tls,omitempty"`
	HeartBeat   string `yaml:"heart_beat,omitempty"`
	ExpireAfter string `yaml:"expire_after,omitempty"`
}

type Admin struct {
	Httpsd Httpsd `yaml:"httpsd,omitempty"`
}

type Tools struct {
	GrafanaAPIToken string `yaml:"grafana_api_token,omitempty"`
	AsupDisabled    bool   `yaml:"autosupport_disabled,omitempty"`
}

type Collector struct {
	Name      string    `yaml:"-"`
	Templates *[]string `yaml:"-"`
}

type CredentialsScript struct {
	Path     string `yaml:"path,omitempty"`
	Schedule string `yaml:"schedule,omitempty"`
	Timeout  string `yaml:"timeout,omitempty"`
}

type Poller struct {
	Addr              string                `yaml:"addr,omitempty"`
	APIVersion        string                `yaml:"api_version,omitempty"`
	APIVfiler         string                `yaml:"api_vfiler,omitempty"`
	AuthStyle         string                `yaml:"auth_style,omitempty"`
	CaCertPath        string                `yaml:"ca_cert,omitempty"`
	ClientTimeout     string                `yaml:"client_timeout,omitempty"`
	Collectors        []Collector           `yaml:"collectors,omitempty"`
	CredentialsFile   string                `yaml:"credentials_file,omitempty"`
	CredentialsScript CredentialsScript     `yaml:"credentials_script,omitempty"`
	Datacenter        string                `yaml:"datacenter,omitempty"`
	Exporters         []string              `yaml:"exporters,omitempty"`
	IsKfs             bool                  `yaml:"is_kfs,omitempty"`
	Labels            *[]*map[string]string `yaml:"labels,omitempty"`
	LogMaxBytes       int64                 `yaml:"log_max_bytes,omitempty"`
	LogMaxFiles       int                   `yaml:"log_max_files,omitempty"`
	LogSet            *[]string             `yaml:"log,omitempty"`
	Password          string                `yaml:"password,omitempty"`
	PollerSchedule    string                `yaml:"poller_schedule,omitempty"`
	SslCert           string                `yaml:"ssl_cert,omitempty"`
	SslKey            string                `yaml:"ssl_key,omitempty"`
	TLSMinVersion     string                `yaml:"tls_min_version,omitempty"`
	UseInsecureTLS    *bool                 `yaml:"use_insecure_tls,omitempty"`
	Username          string                `yaml:"username,omitempty"`
	PreferZAPI        bool                  `yaml:"prefer_zapi,omitempty"`
	promIndex         int
	Name              string
}

// Union merges a poller's config with the defaults.
// For all keys in default, copy them to the poller if the poller does not already include them
func (p *Poller) Union(defaults *Poller) {
	// this is needed because of how mergo handles boolean zero values
	isInsecureNil := true
	var pUseInsecureTLS bool
	pIsKfs := p.IsKfs
	if p.UseInsecureTLS != nil {
		isInsecureNil = false
		pUseInsecureTLS = *p.UseInsecureTLS
	}
	// Don't copy auth related fields from defaults to poller, even when the poller is missing those fields.
	// Save a copy of the poller's auth fields and restore after merge
	pPassword := p.Password
	pAuthStyle := p.AuthStyle
	pCredentialsFile := p.CredentialsFile
	pCredentialsScript := p.CredentialsScript.Path
	_ = mergo.Merge(p, defaults)
	if !isInsecureNil {
		p.UseInsecureTLS = &pUseInsecureTLS
	}
	p.IsKfs = pIsKfs
	p.Password = pPassword
	p.AuthStyle = pAuthStyle
	p.CredentialsFile = pCredentialsFile
	p.CredentialsScript.Path = pCredentialsScript
}

// ZapiPoller creates a poller out of a node, this is a bridge between the node and struct-based code
// Used by ZAPI based code
func ZapiPoller(n *node.Node) *Poller {
	var p Poller

	if Config.Defaults != nil {
		p = *Config.Defaults
	} else {
		p = Poller{}
	}
	p.Name = n.GetChildContentS("poller_name")
	if apiVersion := n.GetChildContentS("api_version"); apiVersion != "" {
		p.APIVersion = apiVersion
	} else {
		if p.APIVersion == "" {
			p.APIVersion = DefaultAPIVersion
		}
	}
	if vfiler := n.GetChildContentS("api_vfiler"); vfiler != "" {
		p.APIVfiler = vfiler
	}
	if addr := n.GetChildContentS("addr"); addr != "" {
		p.Addr = addr
	}
	isKfs := n.GetChildContentS("is_kfs")
	p.IsKfs = isKfs == "true"

	if x := n.GetChildContentS("use_insecure_tls"); x != "" {
		if insecureTLS, err := strconv.ParseBool(x); err == nil {
			// err can be ignored since conf was already validated
			p.UseInsecureTLS = &insecureTLS
		}
	}
	if authStyle := n.GetChildContentS("auth_style"); authStyle != "" {
		p.AuthStyle = authStyle
	}
	if sslCert := n.GetChildContentS("ssl_cert"); sslCert != "" {
		p.SslCert = sslCert
	}
	if sslKey := n.GetChildContentS("ssl_key"); sslKey != "" {
		p.SslKey = sslKey
	}
	if caCert := n.GetChildContentS("ca_cert"); caCert != "" {
		p.CaCertPath = caCert
	}
	if username := n.GetChildContentS("username"); username != "" {
		p.Username = username
	}
	if password := n.GetChildContentS("password"); password != "" {
		p.Password = password
	}
	if credentialsFile := n.GetChildContentS("credentials_file"); credentialsFile != "" {
		p.CredentialsFile = credentialsFile
	}
	if credentialsScriptNode := n.GetChildS("credentials_script"); credentialsScriptNode != nil {
		p.CredentialsScript.Path = credentialsScriptNode.GetChildContentS("path")
		p.CredentialsScript.Schedule = credentialsScriptNode.GetChildContentS("schedule")
		p.CredentialsScript.Timeout = credentialsScriptNode.GetChildContentS("timeout")
	}
	if clientTimeout := n.GetChildContentS("client_timeout"); clientTimeout != "" {
		p.ClientTimeout = clientTimeout
	} else {
		if p.ClientTimeout == "" {
			p.ClientTimeout = DefaultTimeout
		}
	}
	if tlsMinVersion := n.GetChildContentS("tls_min_version"); tlsMinVersion != "" {
		p.TLSMinVersion = tlsMinVersion
	}
	if logSet := n.GetChildS("log"); logSet != nil {
		names := logSet.GetAllChildNamesS()
		p.LogSet = &names
	}
	return &p
}

type Exporter struct {
	Port              *int      `yaml:"port,omitempty"`
	PortRange         *IntRange `yaml:"port_range,omitempty"`
	Type              string    `yaml:"exporter,omitempty"`
	Addr              *string   `yaml:"addr,omitempty"`
	URL               *string   `yaml:"url,omitempty"`
	LocalHTTPAddr     string    `yaml:"local_http_addr,omitempty"`
	GlobalPrefix      *string   `yaml:"global_prefix,omitempty"`
	AllowedAddrs      *[]string `yaml:"allow_addrs,omitempty"`
	AllowedAddrsRegex *[]string `yaml:"allow_addrs_regex,omitempty"`
	CacheMaxKeep      *string   `yaml:"cache_max_keep,omitempty"`
	ShouldAddMetaTags *bool     `yaml:"add_meta_tags,omitempty"`

	// Prometheus specific
	HeartBeatURL string `yaml:"heart_beat_url,omitempty"`
	SortLabels   bool   `yaml:"sort_labels,omitempty"`

	// InfluxDB specific
	Bucket        *string `yaml:"bucket,omitempty"`
	Org           *string `yaml:"org,omitempty"`
	Token         *string `yaml:"token,omitempty"`
	Precision     *string `yaml:"precision,omitempty"`
	ClientTimeout *string `yaml:"client_timeout,omitempty"`
	Version       *string `yaml:"version,omitempty"`
}

type Pollers struct {
	namesInOrder []string
}

var defaultTemplate = &[]string{"default.yaml", "custom.yaml"}

func NewCollector(name string) Collector {
	return Collector{
		Name:      name,
		Templates: defaultTemplate,
	}
}

func (c *Collector) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" {
		c.Name = n.Value
		c.Templates = defaultTemplate
	} else if n.Kind == yaml.MappingNode && len(n.Content) == 2 {
		c.Name = n.Content[0].Value
		var subs []string
		c.Templates = &subs
		seq := n.Content[1]
		for _, n2 := range seq.Content {
			subs = append(subs, n2.Value)
		}
	}
	return nil
}

func (i *Pollers) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.MappingNode {
		var namesInOrder []string
		for _, n := range node.Content {
			if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" {
				namesInOrder = append(namesInOrder, n.Value)
			}
		}
		i.namesInOrder = namesInOrder
	}
	return nil
}

type OrderedConfig struct {
	Pollers Pollers `yaml:"Pollers,omitempty"`
}

type HarvestConfig struct {
	Tools          *Tools              `yaml:"Tools,omitempty"`
	Exporters      map[string]Exporter `yaml:"Exporters,omitempty"`
	Pollers        map[string]*Poller  `yaml:"Pollers,omitempty"`
	Defaults       *Poller             `yaml:"Defaults,omitempty"`
	Admin          Admin               `yaml:"Admin,omitempty"`
	PollersOrdered []string            // poller names in same order as yaml config
}
