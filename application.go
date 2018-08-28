package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"gopkg.in/yaml.v2"
)

type Config struct {
	PixelFormat int
	Width       int
	Height      int
	CameraAddr string
}

var config Config

func help() {
	fmt.Println()
	fmt.Println("capture")
	fmt.Println("broadcast")
}

func main() {
	// read and parse config
	rawConfig, err := ioutil.ReadFile("config.yaml")
	if err != nil {
		panic(err)
	}
	err = yaml.Unmarshal(rawConfig, &config)
	if err != nil {
		panic(err)
	}
	log.Printf("config:\n%#v\n", config)

	if len(os.Args) < 2 {
		help()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "capture":
		capture()
	case "broadcast":
		broadcast()
	default:
		help()
		os.Exit(1)
	}
}
