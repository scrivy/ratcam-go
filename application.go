package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/blackjack/webcam"
	"github.com/getsentry/raven-go"
	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"gopkg.in/yaml.v2"
)

const webcamDevicePath = "/dev/video0"

var (
	camera      *webcam.Webcam
	config      Config
	newConnChan chan client
)

type Config struct {
	PixelFormat int
	Width       int
	Height      int
}

type client struct {
	conn    net.Conn
	picChan chan []byte
	ctx     context.Context
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

	err = openCamera()
	if err != nil {
		panic(err)
	}
	defer camera.Close()
	dumpWebcamFormats()

	newConnChan = make(chan client, 10)
	go takePictures()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		htmlIndex, err := ioutil.ReadFile("index.html")
		if err != nil {
			log.Println(err)
			raven.CaptureError(err, nil)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Write(htmlIndex)
	})
	mux.HandleFunc("/ws", wsHandler)

	log.Println("Listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, _, _, err := ws.UpgradeHTTP(r, w, nil)
	if err != nil {
		log.Println(err)
		raven.CaptureError(err, nil)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	c := client{
		conn:    conn,
		picChan: make(chan []byte, 2),
		ctx:     ctx,
	}
	newConnChan <- c

	// read websocket
	go func() {
		defer func() {
			log.Println("read websocket go routine closed")
		}()
		for {
			select {
			case <-ctx.Done():
				log.Println("read websocket go routine closed from ctx.Done()")
				return
			default:
				msg, op, err := wsutil.ReadClientData(conn)
				if err != nil {
					switch err.(type) {
					case wsutil.ClosedError:
						cancel()
						return
					}
					switch err {
					case io.EOF:
						cancel()
					default:
						log.Println(err)
						raven.CaptureError(err, nil)
					}
					return
				} else {
					ravenMessage := fmt.Sprintf("msg: %#v, op: %#v, err: %#v", msg, op, err)
					raven.CaptureMessage(ravenMessage, nil)
				}
			}
		}
	}()

	// write websocket
	go func() {
		defer func() {
			c.conn.Close()
			close(c.picChan)
			log.Println("write websocket go routine closed")
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case pic := <-c.picChan:
				if len(c.picChan) != 0 {
					log.Println("dropping frame to get more recent pic")
					continue
				}
				err := wsutil.WriteServerMessage(conn, ws.OpText, pic)
				if err != nil {
					cancel()
					log.Println(err)
					switch err.(type) {
					case *net.OpError:
					default:
						raven.CaptureError(err, nil)
					}
					return
				}
				log.Println("served somebody")
			}
		}
	}()
}

func takePictures() {
	var base64image bytes.Buffer
	var streaming bool
	clients := map[net.Conn]client{}

	for {
		select {
		case c := <-newConnChan:
			clients[c.conn] = c
		default:
			if len(clients) == 0 {
				if streaming {
					err := camera.StopStreaming()
					if err != nil {
						log.Println(err)
						raven.CaptureError(err, nil)
					}
					streaming = false
				}
				time.Sleep(250 * time.Millisecond)
				continue
			}

			if !streaming {
				err := camera.StartStreaming()
				if err != nil {
					log.Println(err)
					continue
				}
				streaming = true
			}
			start := time.Now()
			err := camera.WaitForFrame(1)
			if err != nil {
				switch err.(type) {
				case *webcam.Timeout:
				default:
					log.Println(err)
					raven.CaptureError(err, nil)
				}
				continue
			}

			frame, err := camera.ReadFrame()
			if err != nil {
				log.Println(err)
				raven.CaptureError(err, nil)
				continue
			}

			rawImage := frameToYCbCr(&frame)

			base64Encoder := base64.NewEncoder(base64.StdEncoding, &base64image)
			err = jpeg.Encode(base64Encoder, rawImage, nil)
			if err != nil {
				log.Println(err)
				raven.CaptureError(err, nil)
				base64image.Reset()
				continue
			}
			base64Encoder.Close()

			latestPicture := base64image.Bytes()

			for _, c := range clients {
				if c.ctx.Err() == nil {
					if len(c.picChan) != cap(c.picChan) {
						c.picChan <- latestPicture
					} else {
						log.Println("there's one in the chamber")
					}
				} else {
					delete(clients, c.conn)
				}
			}

			log.Printf("captured image in %s", time.Since(start).String())
			base64image.Reset()
		}
	}
}

func openCamera() (err error) {
	camera, err = webcam.Open(webcamDevicePath)
	if err != nil {
		return
	}
	_, _, _, err = camera.SetImageFormat(webcam.PixelFormat(config.PixelFormat), uint32(config.Width), uint32(config.Height))
	if err != nil {
		return
	}
	err = camera.SetAutoWhiteBalance(true)
	return
}

func dumpWebcamFormats() {
	for pf, info := range camera.GetSupportedFormats() {
		log.Printf("\n\npixelFormat: %v %s, frame sizes:\n", pf, info)
		for _, size := range camera.GetSupportedFrameSizes(pf) {
			log.Printf("%#v\n", size)
		}
	}
}

func frameToYCbCr(frame *[]byte) image.Image {
	yuyv := image.NewYCbCr(image.Rect(0, 0, config.Width, config.Height), image.YCbCrSubsampleRatio422)
	frameLength := len(*frame)
	for i := range yuyv.Cb {
		ii := i * 4
		if ii+3 >= frameLength {
			break
		}
		yuyv.Y[i*2] = (*frame)[ii]
		yuyv.Y[i*2+1] = (*frame)[ii+2]
		yuyv.Cb[i] = (*frame)[ii+1]
		yuyv.Cr[i] = (*frame)[ii+3]
	}
	return yuyv
}
