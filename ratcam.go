package main

import (
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Mode                     string
	Debug                    bool
	PixelFormat              int
	WebcamDevicePath         string
	Width                    int
	Height                   int
	CameraIP                 string
	CameraPort               string
	HomeIPv6                 string
	RedirectToLocal          bool `yaml:"redirect_to_local"`
	LocalAddr                string
	MaxStreamDurationMinutes int    `yaml:"max_stream_duration_minutes"`
	BroadcastPort            string `yaml:"broadcast_port"`
}

var config Config

func main() {
	showHelp := flag.Bool("h", false, "show help")
	configPath := flag.String("c", "./config.yaml", "config path")
	indexHtmlPath := flag.String("htmlpath", "./index.html", "index.html path")
	flag.Parse()

	if *showHelp {
		flag.PrintDefaults()
		os.Exit(0)
	}

	// load config
	configFile, err := os.Open(*configPath)
	if err != nil {
		panic(err)
	}
	yamlDec := yaml.NewDecoder(configFile)
	yamlDec.SetStrict(true)
	err = yamlDec.Decode(&config)
	if err != nil {
		panic(err)
	}
	if config.Debug {
		fmt.Printf("config:\n%#v\n", config)
	}

	// for profiling
	if config.Debug {
		go func() {
			fmt.Println(http.ListenAndServe("localhost:6060", nil))
		}()
	}

	// split the service into 2 nodes
	switch config.Mode {
	case "capture":
		capture()
	case "broadcast":
		broadcast(*indexHtmlPath)
	case "both":
		// run both nodes in the same process
		go broadcast(*indexHtmlPath)
		capture()
	default:
		fmt.Printf("mode not supported. both, capture, or broadcast. Is it set in the yaml config?")
		os.Exit(1)
	}
}
