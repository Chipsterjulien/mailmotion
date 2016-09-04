package main

import (
	"bitbucket.org/zombiezen/cardcpx/natsort"
	"fmt"
	"github.com/jordan-wright/email"
	"github.com/op/go-logging"
	"github.com/spf13/viper"
	"net/smtp"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type piece struct {
	Email   *email.Email
	Picture string
}

func main() {
	confPath := "/etc/mailmotion/"
	confFilename := "mailmotion"
	logFilename := "/var/log/mailmotion/error.log"

	// confPath := "cfg/"
	// confFilename := "mailmotion"
	// logFilename := "error.log"

	fd := initLogging(&logFilename)
	defer fd.Close()

	loadConfig(&confPath, &confFilename)
	startApp()
}

func changePathList(pictureList *[]string, newPath *string) {
	log := logging.MustGetLogger("log")

	for num, pic := range *pictureList {
		_, filename := path.Split(pic)
		(*pictureList)[num] = path.Join(*newPath, filename)

		log.Debugf("\"%s\" was changed to \"%s\"", pic, (*pictureList)[num])
	}
}

func createEmail(pic *string) *email.Email {
	e := email.NewEmail()
	e.From = viper.GetString("email.from")
	e.To = viper.GetStringSlice("email.sendTo")
	e.Subject = viper.GetString("email.subject")
	e.AttachFile(*pic)

	return e
}

func createPath(newPath *string) {
	log := logging.MustGetLogger("log")

	if err := os.MkdirAll(*newPath, 0744); err != nil {
		log.Criticalf("Unable to create directories: %s", err)
		os.Exit(1)
	}

	log.Debugf("Create new path: %s", *newPath)
}

func findPicture() []string {
	log := logging.MustGetLogger("log")

	path := viper.GetString("default.picturePath")

	types := []string{"*.jpg", "*.JPG", "*.ppm", "*.PPM"}
	pictureList := []string{}

	log.Debugf("Pictures extensions: %v", types)
	log.Debugf("Picture path: %s", path)

	for _, ext := range types {
		someFiles, err := filepath.Glob(filepath.Join(path, ext))
		log.Debugf("glob: %v", filepath.Join(path, ext))
		log.Debugf("pictures files: %v", someFiles)
		if err != nil {
			log.Warningf("Unable to find pictures: %v", err)
		}

		for _, onePicture := range someFiles {
			pictureList = append(pictureList, onePicture)
		}
	}

	natsort.Strings(pictureList)
	log.Debugf("pictureList: %v", pictureList)

	return pictureList
}

func launchShipping(pool *email.Pool, ch chan piece, removeChan chan<- string) {
	for i := 0; i < 2; i++ {
		go func(num int, pool *email.Pool, ch chan piece, removeChan chan<- string) {
			log := logging.MustGetLogger("log")

			timeout := 10 * time.Second

			for e := range ch {
				err := pool.Send(e.Email, timeout)
				if err != nil {
					errSplitted := strings.Split(err.Error(), " ")

					switch errSplitted[0] {
					case "421":
						ch <- e
						time.Sleep(120 * time.Second)
					case "550":
						fmt.Println("I can't send email anymore !")
						os.Exit(1)
					}
				} else {
					log.Debug("Email was sent")

					removeChan <- e.Picture
				}
			}
		}(i, pool, ch, removeChan)
	}
}

func moveFiles(pictureList *[]string, newPath *string) {
	log := logging.MustGetLogger("log")

	for _, pic := range *pictureList {
		_, filename := path.Split(pic)
		np := path.Join(*newPath, filename)
		if err := os.Rename(pic, np); err != nil {
			log.Criticalf("Unable to move \"%s\" to \"%s\" !", pic, np)
			os.Exit(1)
		} else {
			log.Debugf("\"%s\" was moved to \"%s\"", pic, np)
		}
	}
}

func removePicture(removeChan <-chan string) {
	log := logging.MustGetLogger("log")

	for true {
		picture := <-removeChan
		os.Remove(picture)

		log.Debugf("Picture \"%s\" was removed", picture)
	}
}

func startApp() {
	log := logging.MustGetLogger("log")

	emailChan := make(chan piece, 80)
	removeChan := make(chan string, 80)

	mySmtp := viper.GetString("email.smtp")
	host := fmt.Sprintf("%s:%d", mySmtp, viper.GetInt("email.port"))
	login := viper.GetString("email.login")
	password := viper.GetString("email.password")
	sleepBefore := viper.GetInt("default.sleepBeforeStarting")
	sleep := time.Duration(viper.GetInt("default.sleepTime")) * time.Second
	defaultPath := viper.GetString("default.picturePath")
	newPath := path.Join(defaultPath, "send")

	log.Debugf("Time waiting between 2 loops: %.0fs", sleep.Seconds())

	createPath(&newPath)

	pool := email.NewPool(host, 4, smtp.PlainAuth("", login, password, mySmtp))
	// Start remove picture goroutine
	go removePicture(removeChan)

	log.Debugf("Before starting to work, waiting \"%ds\"", sleepBefore)
	// Sleep before starting
	time.Sleep(time.Duration(sleepBefore) * time.Second)

	log.Debug("After sleeping at the beginning")
	log.Debug("Removing all pictures before starting")

	// Remove picture before starting
	pictureList := findPicture()
	for _, picture := range pictureList {
		removeChan <- picture
	}

	launchShipping(pool, emailChan, removeChan)
	time.Sleep(sleep)

	for {
		pictureList = findPicture()
		moveFiles(&pictureList, &newPath)
		changePathList(&pictureList, &newPath)

		for _, pic := range pictureList {
			e := createEmail(&pic)
			emailChan <- piece{
				Email:   e,
				Picture: pic,
			}
		}
		// wait before start another pictures search
		time.Sleep(sleep)
	}
}
