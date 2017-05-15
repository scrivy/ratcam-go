package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/raven-go"
)

func main() {
	index, err := ioutil.ReadFile("index.html")
	if err != nil {
		logErr(err)
		log.Fatalln("problem loading index.html")
	}

	incoming := make(chan bool)

	go takePictures(incoming)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, http.StatusText(405), 405)
			return
		}
		w.Write(index)
	})

	http.HandleFunc("/latest.jpeg", func(w http.ResponseWriter, r *http.Request) {
		picBytes, err := getLatestPicture(incoming)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(picBytes)
		log.Println("served somebody")
	})

	log.Println("Listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

var (
	latestPicture    []byte
	pictureMutex     = &sync.RWMutex{}
	capturingPic     bool
	capturingPicLock = &sync.RWMutex{}
	capturingPicWG   = &sync.WaitGroup{}
)

func takePictures(incomingReq chan bool) {
	var imageBytes, stdErr bytes.Buffer
	var start time.Time
	var err error
	for {
		select {
		case <-incomingReq:
			start = time.Now()
			cmd := exec.Command("fswebcam", "-r", "1920x1080", "--jpeg", "90", "-q", "-")
			cmd.Stdout = &imageBytes
			cmd.Stderr = &stdErr
			err = cmd.Run()
			if err != nil {
				logErr(err)
				capturingPicWG.Done()
				continue
			}
			elapsed := time.Since(start)
			fmt.Printf("\n%s\n", stdErr.String())
			if strings.Contains(stdErr.String(), "unrecoverable error") {
				logErr(errors.New(stdErr.String()))
			} else if strings.Contains(stdErr.String(), "Error opening device") {
				logErr(errors.New(stdErr.String()))
			} else if strings.Contains(stdErr.String(), "No such file or directory") {
				logErr(errors.New(stdErr.String()))
			} else if len(stdErr.String()) > 0 {
				logErr(errors.New(stdErr.String()))
			} else {
				pictureMutex.Lock()
				latestPicture = imageBytes.Bytes()
				pictureMutex.Unlock()
				log.Printf("captured image in %s", elapsed)
			}
			imageBytes.Reset()
			stdErr.Reset()
			capturingPicLock.Lock()
			capturingPic = false
			capturingPicLock.Unlock()
			capturingPicWG.Done()
		}
	}
}

func getLatestPicture(incoming chan bool) (picture []byte, err error) {
	capturingPicLock.RLock()
	capturing := capturingPic
	capturingPicLock.RUnlock()

	if !capturing {
		capturingPicLock.Lock()
		capturingPic = true
		capturingPicWG.Add(1)
		capturingPicLock.Unlock()
		incoming <- true
	}

	capturingPicWG.Wait()

	pictureMutex.RLock()
	picture = latestPicture
	pictureMutex.RUnlock()

	if len(picture) == 0 {
		err = errors.New("bad gorilla")
	}
	return
}

func logErr(err error) {
	log.Println(err)
	raven.CaptureErrorAndWait(err, nil)
}
