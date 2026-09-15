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
		TemporaryPath    string `yaml:"temp"`
		// KnownHostsPath is the OpenSSH known_hosts file used to verify the
		// host key of every SSH/SFTP endpoint the transports and scoopers
		// connect to (see common.HostKeyCallback).
		//
		// There is deliberately NO default: an empty value means "not
		// configured", and unless SftpInsecureIgnoreHostKey is also set the
		// SSH code fails closed with an error naming both options rather
		// than trusting an unknown host key.
		KnownHostsPath string `yaml:"known-hosts"`
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
	// Queue configures how work reaches the worker pool
	// (jobqueue/jobqueue.go). Two triggers feed it: an insert enqueues the row
	// it just stored, and this poller is the safety net for rows inserted by
	// any other path and for work that was waiting across a restart.
	Queue struct {
		// PollEnabled turns the safety-net poller on or off. It is true by
		// default, like the Java original, whose ControlThread always polled
		// (ControlThread.java:77-100) - a deployment has to opt out
		// deliberately, and with the poller off only the on-insert trigger
		// runs, so rows written by another path are never picked up.
		PollEnabled bool `yaml:"poll-enabled"`
		// PollIntervalMs is how long the poller waits between passes over
		// tPayload. The Java's SLEEP_TIME default was 500 ms
		// (ControlThread.java:69, `protected int SLEEP_TIME = 500;`), which is
		// what this defaults to; a value <= 0 falls back to that default.
		PollIntervalMs int `yaml:"poll-interval-ms"`
	} `yaml:"queue"`
	// SftpInsecureIgnoreHostKey is the explicit opt-in to skipping SSH host
	// key verification for the SFTP transports and scoopers. It exists for
	// first contact (capturing a host key into paths.known-hosts) and for
	// tests; it is false by default, so an operator has to write it out
	// deliberately, and while it is in effect every SSH/SFTP peer is trusted
	// without any host key check (a loud warning is logged).
	//
	// When paths.known-hosts is ALSO configured the known_hosts file wins:
	// this flag is a bypass of last resort, never an override.
	SftpInsecureIgnoreHostKey bool `yaml:"sftp-insecure-ignore-hostkey"`
	InternalXslt              bool `yaml:"internal-xslt"`
}

func (c *AppConfig) SetDefaults() {
	c.Port = 3000
	c.Database.Name = "remitt"
	c.Database.User = "remitt"
	c.Database.Pass = "remitt"
	c.Database.Host = "localhost"
	c.Paths.BasePath = "."
	c.Paths.DbMigrationsPath = "migrations"
	c.Paths.TemporaryPath = "/tmp"
	// Worker threads: with NO default, a configuration that omits
	// timing-iterations started StartDispatcher(0) - zero workers, so the
	// dispatcher accepted work and blocked forever waiting for a free worker
	// while nothing was ever processed and no error was logged. Measured on
	// 2026-09-15 with a payload stuck at 'valid' and a tProcessor row with
	// threadId 0. The sample remitt.yml uses 16; 4 is a working default.
	c.TimingIterations.NumWorkerThreads = 4

	// The queue's safety-net poller: on, every 500 ms - the Java original's
	// SLEEP_TIME (ControlThread.java:69) and the interval its run() loop used
	// to look for unassigned payloads (see jobqueue/poller.go).
	c.Queue.PollEnabled = true
	c.Queue.PollIntervalMs = 500
	// The in-process XSLT engine (ratago + the forked xpath) is the default: it
	// matches xsltproc byte-for-byte on the shipped stylesheets and needs no
	// external process. Set `internal-xslt: false` to use the xsltproc binary
	// instead; the binary is still shipped and is also used as a fallback if the
	// in-process engine errors (see common.XslTransform).
	c.InternalXslt = true

	// Paths.KnownHostsPath and SftpInsecureIgnoreHostKey are deliberately left
	// at their zero values: there is no default known_hosts file (a guessed
	// path would either fail confusingly or, worse, verify against something
	// the operator never chose) and host key verification is never silently
	// disabled. With neither option configured the SSH code fails closed; see
	// common.HostKeyCallback.
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
