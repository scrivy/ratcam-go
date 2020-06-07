package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	Mode                     string
	Debug                    bool
	PixelFormat              int
	Width                    int
	Height                   int
	CameraAddr               string
	HomeIp                   string
	LocalAddr                string
	MaxStreamDurationMinutes int
}

var config Config

func main() {
	// for profiling
	if config.Debug {
		go func() {
			log.Println(http.ListenAndServe("localhost:6060", nil))
		}()
	}

	rawConfig, err := ioutil.ReadFile("/etc/ratcam.yaml")
	if err != nil {
		panic(err)
	}
	err = yaml.Unmarshal(rawConfig, &config)
	if err != nil {
		panic(err)
	}
	if config.Debug {
		fmt.Printf("%#v\n", config)
	}

	// split the service into 2 nodes
	switch config.Mode {
	case "capture":
		capture()
	case "broadcast":
		broadcast()
	case "both":
		// run both nodes in the same process, default to localhost
		config.CameraAddr = "127.0.0.1:5005"
		go broadcast()
		capture()
	default:
		fmt.Printf("mode not supported. Is it set in the config yaml?")
		os.Exit(1)
	}
	return
}
