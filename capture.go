package main

import (
	"bufio"
	"encoding/gob"
	"fmt"
	"log"
	"net"

	"github.com/blackjack/webcam"
	"github.com/getsentry/raven-go"
	"github.com/pkg/errors"
)


const webcamDevicePath = "/dev/video0"

var camera *webcam.Webcam

func capture() {
	// open camera
	var err error
	camera, err = webcam.Open(webcamDevicePath)
	if err != nil {
		panic(err)
	}
	// dump supported output formats
	for pf, info := range camera.GetSupportedFormats() {
		log.Printf("\n\npixelFormat: %v %s, frame sizes:\n", pf, info)
		for _, size := range camera.GetSupportedFrameSizes(pf) {
			log.Printf("%#v\n", size)
		}
	}
	// set output format
	_, _, _, err = camera.SetImageFormat(webcam.PixelFormat(config.PixelFormat), uint32(config.Width), uint32(config.Height))
	if err != nil {
		panic(err)
	}
	err = camera.SetAutoWhiteBalance(true)
	if err != nil {
		panic(err)
	}

	// listen for incoming requests
	listener, err := net.Listen("tcp", ":5005")
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("%+v\n", errors.WithStack(err))
			continue
		}
		sendFrames(conn)
	}
}

func sendFrames(conn net.Conn) {
	defer func() {
		err := camera.StopStreaming()
		if err != nil {
			fmt.Printf("%+v\n", errors.WithStack(err))
			raven.CaptureError(err, nil)
		}
		conn.Close()
		log.Println("stopped streaming")
	}()

	err := camera.StartStreaming()
	if err != nil {
		fmt.Printf("%+v\n", errors.WithStack(err))
		raven.CaptureError(err, nil)
		return
	}
	log.Println("streaming")

	w := bufio.NewWriterSize(conn, 200000)
	encoder := gob.NewEncoder(w)

	for {
		err := camera.WaitForFrame(1)
		if err != nil {
			switch err.(type) {
			case *webcam.Timeout:
			default:
				fmt.Printf("%+v\n", errors.WithStack(err))
				raven.CaptureError(err, nil)
			}
			return
		}

		frame, err := camera.ReadFrame()
		if err != nil {
			fmt.Printf("%+v\n", errors.WithStack(err))
			raven.CaptureError(err, nil)
			return
		}

		err = encoder.Encode(frame)
		if err != nil {
			fmt.Printf("%+v\n", errors.WithStack(err))
			raven.CaptureError(err, nil)
			return
		}

	}
}
