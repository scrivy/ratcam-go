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
	PixelFormat int
	Width       int
	Height      int
	CameraAddr  string
}

var config Config

func help() {
	fmt.Printf(`
cli help

broadcast
capture

`)
}

func main() {
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	// read and parse config
	rawConfig, err := ioutil.ReadFile("config.yaml")
	if err != nil {
		panic(err)
	}
	err = yaml.Unmarshal(rawConfig, &config)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%#v\n", config)

	// run both in the same process, default to localhost
	if len(os.Args) < 2 {
		config.CameraAddr = "127.0.0.1:5005"
		go broadcast()
		capture()
		return
	}

	// split the service into 2 nodes
	switch os.Args[1] {
	case "capture":
		capture()
	case "broadcast":
		broadcast()
	default:
		help()
	}
}
