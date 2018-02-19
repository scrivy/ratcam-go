package main

import (
	"bytes"
	"image"
	"image/jpeg"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/blackjack/webcam"
	//	"github.com/gobwas/ws"
	//	"github.com/gobwas/ws/wsutil"
	"gopkg.in/yaml.v2"
)

const webcamDevicePath = "/dev/video0"

var (
	camera          *webcam.Webcam
	config          Config
	latestPicture   []byte
	pictureMutex    = &sync.RWMutex{}
	lastRequestChan chan bool
)

type Config struct {
	PixelFormat int
	Width       int
	Height      int
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

	htmlIndex, err := ioutil.ReadFile("index.html")
	if err != nil {
		panic(err)
	}

	err = openCamera()
	if err != nil {
		panic(err)
	}
	defer camera.Close()
	dumpWebcamFormats()

	lastRequestChan = make(chan bool, 20)
	go takePictures()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write(htmlIndex)
	})
	mux.HandleFunc("/latest.jpeg", picHandler)

	log.Println("Listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func picHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/jpeg")

	pictureMutex.RLock()
	reqPic := make([]byte, len(latestPicture))
	copy(reqPic, latestPicture)
	pictureMutex.RUnlock()

	w.Write(reqPic)

	lastRequestChan <- true

	log.Println("served somebody")
}

func takePictures() {
	var frame []byte
	var jpegBuffer bytes.Buffer
	var rawImage image.Image
	var start time.Time
	lastRequest := time.Now()
	var err error
	var streaming bool

	for {
		select {
		case <-lastRequestChan:
			lastRequest = time.Now()
		default:
			if !lastRequest.Add(5 * time.Second).After(time.Now()) {
				if streaming {
					err = camera.StopStreaming()
					if err != nil {
						log.Println(err.Error())
					}
					streaming = false
				}
				time.Sleep(500 * time.Millisecond)
				continue
			}

			pictureMutex.Lock()
			if !streaming {
				err = camera.StartStreaming()
				if err != nil {
					log.Println("camera.StartStreaming(): ", err)
					pictureMutex.Unlock()
					continue
				}
				streaming = true
			}
			start = time.Now()
			err = camera.WaitForFrame(1)
			if err != nil {
				pictureMutex.Unlock()
				switch err.(type) {
				case *webcam.Timeout:
				default:
					log.Println(err.Error())
				}
				continue
			}

			frame, err = camera.ReadFrame()
			if err != nil {
				pictureMutex.Unlock()
				log.Println(err.Error())
				continue
			}

			rawImage = frameToYCbCr(&frame)
			err = jpeg.Encode(&jpegBuffer, rawImage, nil)
			if err != nil {
				pictureMutex.Unlock()
				log.Println(err.Error())
				jpegBuffer.Reset()
				continue
			}

			latestPicture = jpegBuffer.Bytes()

			pictureMutex.Unlock()
			log.Printf("captured image in %s", time.Since(start).String())
			jpegBuffer.Reset()
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
