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

	incoming := make(chan bool, 100)

	go takePictures(incoming)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, http.StatusText(405), 405)
			return
		}
		w.Write(index)
		incoming <- true
	})

	http.HandleFunc("/latest.jpeg", func(w http.ResponseWriter, r *http.Request) {
		picBytes, err := getLatestPicture()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(picBytes)
		incoming <- true
		log.Println("served somebody")
	})

	log.Println("Listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

var (
	latestPicture []byte
	pictureMutex  *sync.RWMutex = &sync.RWMutex{}
)

func takePictures(incomingReq chan bool) {
	var imageBytes, stdErr bytes.Buffer
	var start time.Time
	var err error
	countDown := uint8(1)
	for {
		select {
		case <-incomingReq:
			countDown = 5
		default:
			if countDown > 0 {
				start = time.Now()
				cmd := exec.Command("fswebcam", "-r", "1920x1080", "--jpeg", "90", "-q", "-")
				cmd.Stdout = &imageBytes
				cmd.Stderr = &stdErr
				err = cmd.Run()
				if err != nil {
					logErr(err)
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
				countDown--
			} else {
				time.Sleep(500 * time.Millisecond)
			}
		}
	}
}

func getLatestPicture() (picture []byte, err error) {
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
