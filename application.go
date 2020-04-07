package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	PixelFormat int
	Width       int
	Height      int
	CameraAddr  string
	HomeIp      string
	LocalAddr   string
}

var (
	config Config
	DEBUG  bool
)

func main() {
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	// read and parse config
	pwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	rawConfig, err := ioutil.ReadFile(pwd + "/config.yaml")
	if err != nil {
		panic(err)
	}
	err = yaml.Unmarshal(rawConfig, &config)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%#v\n", config)

	// cli flags
	debug := flag.Bool("debug", false, "enables debug logging")
	mode := flag.String("mode", "both", "capture or broadcast")
	flag.Parse()
	DEBUG = *debug

	// split the service into 2 nodes
	switch *mode {
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
		fmt.Printf("Invalid mode: %s, help: -h\n", *mode)
		flag.PrintDefaults()
		os.Exit(1)
	}
	return
}
