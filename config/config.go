package config

import (
	"os"

	"gopkg.in/yaml.v2"
)

var (
	Config *AppConfig
)

type AppConfig struct {
	Debug    bool `yaml:"debug"`
	Port     int  `yaml:"port"`
	Database struct {
		Name string `yaml:"name"`
		User string `yaml:"user"`
		Pass string `yaml:"pass"`
		Host string `yaml:"host"`
	} `yaml:"database"`
	Paths struct {
		BasePath         string `yaml:"base"`
		DbMigrationsPath string `yaml:"db-migrations"`
		DistPath         string `yaml:"dist"`
		XsltProcPath     string `yaml:"xsltproc"`
		SshPath          string `yaml:"ssh"`
		TemporaryPath    string `yaml:"temp"`
	} `yaml:"paths"`
	Mail struct {
		Server      string `yaml:"server"`
		Port        int    `yaml:"port"`
		TLS         bool   `yaml:"tls"`
		FromAddress string `yaml:"from"`
	} `yaml:"mail"`
	TimingIterations struct {
		NumWorkerThreads int `yaml:"worker-threads"`
	} `yaml:"timing-iterations"`
	InternalXslt bool `yaml:"internal-xslt"`
}

func (c *AppConfig) SetDefaults() {
	c.Port = 3000
	c.Database.Name = "remitt"
	c.Database.User = "remitt"
	c.Database.Pass = "remitt"
	c.Database.Host = "localhost"
	c.Paths.BasePath = "."
	c.Paths.DbMigrationsPath = "migrations"
	c.Paths.SshPath = "/usr/bin/ssh"
	c.Paths.TemporaryPath = "/tmp"
	c.InternalXslt = false
}

func LoadConfigWithDefaults(configPath string) (*AppConfig, error) {
	c := &AppConfig{}
	c.SetDefaults()
	data, err := os.ReadFile(configPath)
	if err != nil {
		return c, err
	}
	err = yaml.Unmarshal([]byte(data), c)
	return c, err
}
