package config

import (
	"gopkg.in/yaml.v2"
	"io/ioutil"
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
	} `yaml: "database"`
	Paths struct {
		BasePath string `yaml:"base"`
		DbMigrationsPath string `yaml:"db-migrations"`
		DistPath         string `yaml:"dist"`
		AppBaseDir       string `yaml:"app-base"`
		XsltProcPath     string `yaml:"xsltproc"`
		RsyncPath        string `yaml:"rsync"`
		SshPath          string `yaml:"ssh"`
	} `yaml:"paths"`
	TimingIterations struct {
		NumWorkerThreads int `yaml:"worker-threads"`
		StatusIterations int `yaml:"status-iterations"`
		StatusInterval   int `yaml:"status-interval"`
		StatusTimeout    int `yaml:"status-timeout"`
		LbPollInterval   int `yaml:"lb-poll-interval"`
	} `yaml:"timing-iterations"`
	InternalXslt      bool   `yaml:"internal-xslt"`
}

func (c *AppConfig) SetDefaults() {
	c.Port = 3000
	c.Database.Name = "remitt"
	c.Database.User = "remitt"
	c.Database.Pass = "remitt"
	c.Database.Host = "localhost"
	c.Paths.BasePath = "."
	c.Paths.DbMigrationsPath = "migrations"
	c.Paths.DistPath = "/dist"
	c.Paths.AppBaseDir = "/run"
	c.Paths.RsyncPath = "/usr/bin/rsync"
	c.Paths.SshPath = "/usr/bin/ssh"
	c.InternalXslt = false
}

func LoadConfigWithDefaults(configPath string) (*AppConfig, error) {
	c := &AppConfig{}
	c.SetDefaults()
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return c, err
	}
	err = yaml.Unmarshal([]byte(data), c)
	return c, err
}
