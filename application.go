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
	"gopkg.in/yaml.v2"
)

const webcamDevicePath = "/dev/video0"

var (
	config          Config
	latestPicture   []byte
	pictureMutex    = &sync.RWMutex{}
	lastRequest     = time.Now()
	lastRequestLock = &sync.RWMutex{}
)

type Config struct {
	PixelFormat int
	Width       int
	Height      int
}

func main() {
	dumpWebcamFormats()

	rawConfig, err := ioutil.ReadFile("config.yaml")
	if err != nil {
		panic(err)
	}
	err = yaml.Unmarshal(rawConfig, &config)
	if err != nil {
		panic(err)
	}

	index, err := ioutil.ReadFile("index.html")
	if err != nil {
		panic(err)
	}

	go takePictures()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, http.StatusText(405), 405)
			return
		}
		w.Write(index)
	})

	http.HandleFunc("/latest.jpeg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		pictureMutex.RLock()
		w.Write(latestPicture)
		pictureMutex.RUnlock()

		lastRequestLock.Lock()
		lastRequest = time.Now()
		lastRequestLock.Unlock()

		log.Println("served somebody")
	})

	log.Println("Listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func takePictures() {
	var frame []byte
	var jpegBuffer bytes.Buffer
	var rawImage image.Image
	var start time.Time
	picTimeout := uint32(2)

	cam, err := webcam.Open("/dev/video0")
	if err != nil {
		panic(err)
	}
	defer cam.Close()

	err = cam.SetAutoWhiteBalance(true)
	if err != nil {
		panic(err)
	}

	_, _, _, err = cam.SetImageFormat(webcam.PixelFormat(config.PixelFormat), uint32(config.Width), uint32(config.Height))
	if err != nil {
		panic(err)
	}

	err = cam.StartStreaming()
	if err != nil {
		panic(err)
	}

	for {
		lastRequestLock.RLock()
		if !lastRequest.Add(5 * time.Second).After(time.Now()) {
			time.Sleep(500 * time.Millisecond)
			lastRequestLock.RUnlock()
			continue
		}
		lastRequestLock.RUnlock()

		pictureMutex.Lock()
		start = time.Now()
		err = cam.WaitForFrame(picTimeout)
		if err != nil {
			pictureMutex.Unlock()
			switch err.(type) {
			case *webcam.Timeout:
			default:
				log.Println(err.Error())
			}
			continue
		}

		frame, err = cam.ReadFrame()
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

func dumpWebcamFormats() {
	cam, err := webcam.Open(webcamDevicePath)
	if err != nil {
		panic(err)
	}
	defer cam.Close()

	for pf, info := range cam.GetSupportedFormats() {
		log.Printf("\n\npixelFormat: %v %s, frame sizes:\n", pf, info)
		for _, size := range cam.GetSupportedFrameSizes(pf) {
			log.Printf("%#v\n", size)
		}
	}
}

func frameToYCbCr(frame *[]byte) image.Image {
	yuyv := image.NewYCbCr(image.Rect(0, 0, config.Width, config.Height), image.YCbCrSubsampleRatio422)
	for i := range yuyv.Cb {
		ii := i * 4
		yuyv.Y[i*2] = (*frame)[ii]
		yuyv.Y[i*2+1] = (*frame)[ii+2]
		yuyv.Cb[i] = (*frame)[ii+1]
		yuyv.Cr[i] = (*frame)[ii+3]
	}
	return yuyv
}
