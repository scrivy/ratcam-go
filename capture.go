package main

import (
	"context"
	"encoding/gob"
	"fmt"
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
	if config.Debug {
		for pf, info := range camera.GetSupportedFormats() {
			fmt.Printf("\n\npixelFormat: %v %s, frame sizes:\n", pf, info)
			for _, size := range camera.GetSupportedFrameSizes(pf) {
				fmt.Printf("%#v\n", size)
			}
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
	listener, err := net.Listen("tcp", ":"+config.CameraPort)
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
			fmt.Printf("%+v\n", errors.WithStack(err))
		}
		conn.Close()
		fmt.Println("stopped streaming")
	}()

	err := camera.StartStreaming()
	if err != nil {
		fmt.Printf("%+v\n", errors.WithStack(err))
		return
	}
	fmt.Println("streaming")

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
					fmt.Printf("%+v\n", errors.WithStack(err))
				}
				return
			}

			frame, err := camera.ReadFrame()
			if err != nil {
				fmt.Printf("%+v\n", errors.WithStack(err))
				return
			}
			if len(queueFrames) < cap(queueFrames) {
				queueFrames <- &frame
			} else if config.Debug {
				fmt.Println("queueFrames channel is full")
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
				if config.Debug {
					fmt.Printf("%+v\n", errors.WithStack(err))
				}
				return
			}
		}
	}

}
