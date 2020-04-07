package main

import (
	"context"
	"encoding/gob"
	"log"
	"net"

	"github.com/blackjack/webcam"
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
			log.Printf("%+v\n", errors.WithStack(err))
			continue
		}
		getAndSendFrames(conn)
	}
}

func getAndSendFrames(conn net.Conn) {
	ctx, cancel := context.WithCancel(context.Background())
	queueFrames := make(chan *[]byte, 1)

	go sendFrames(ctx, conn, queueFrames, cancel)

	defer func() {
		cancel()
		close(queueFrames)
		err := camera.StopStreaming()
		if err != nil {
			log.Printf("%+v\n", errors.WithStack(err))
		}
		conn.Close()
		log.Println("stopped streaming")
	}()

	err := camera.StartStreaming()
	if err != nil {
		log.Printf("%+v\n", errors.WithStack(err))
		return
	}
	log.Println("streaming")

	for {
		select {
		case <-ctx.Done():
			return
		default:
			err := camera.WaitForFrame(1)
			if err != nil {
				switch err.(type) {
				case *webcam.Timeout:
				default:
					log.Printf("%+v\n", errors.WithStack(err))
				}
				return
			}

			frame, err := camera.ReadFrame()
			if err != nil {
				log.Printf("%+v\n", errors.WithStack(err))
				return
			}
			if len(queueFrames) < cap(queueFrames) {
				queueFrames <- &frame
			} else if DEBUG {
				log.Println("queueFrames channel is full")
			}
		}
	}
}

func sendFrames(ctx context.Context, conn net.Conn, queueFrames chan *[]byte, cancel context.CancelFunc) {
	defer cancel()

	encoder := gob.NewEncoder(conn)

	var err error
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-queueFrames:
			err = encoder.Encode(*frame)
			if err != nil {
				if DEBUG {
					log.Printf("%+v\n", errors.WithStack(err))
				}
				return
			}
		}
	}

}
