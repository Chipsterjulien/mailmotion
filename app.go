package main

import (
	"fmt"
	"net/smtp"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"bitbucket.org/zombiezen/cardcpx/natsort"

	"github.com/jordan-wright/email"
	logging "github.com/op/go-logging"
	"github.com/spf13/viper"
)

type attachment struct {
	email       *email.Email
	pictureName string
}

func main() {
	confPath := "/etc/mailmotion"
	confFilename := "mailmotion"
	logFilename := "/var/log/mailmotion/error.log"

	// confPath := "cfg"
	// confFilename := "mailmotion_sample"
	// logFilename := "error.log"

	fd := initLogging(&logFilename)
	defer fd.Close()

	loadConfig(&confPath, &confFilename)
	startApp()
}

func addUpperExtension(extensionListPtr *[]string) {
	for _, ext := range *extensionListPtr {
		*extensionListPtr = append(*extensionListPtr, strings.ToUpper(ext))
	}
}

func createAnEmail(pic *string) *email.Email {
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
		log.Criticalf("Unable to create \"%s\" directorie(s): %s", *newPath, err)
		os.Exit(1)
	}

	log.Debugf("Create new path: %s", *newPath)
}

func findPicture() []string {
	log := logging.MustGetLogger("log")

	picturePath := viper.GetString("default.picturePath")
	pictureExtList := viper.GetStringSlice("default.pictureExt")
	picturesList := []string{}

	addUpperExtension(&pictureExtList)

	for _, ext := range pictureExtList {
		fileList, err := filepath.Glob(filepath.Join(picturePath, ext))
		if err != nil {
			log.Warningf("Unable to launch a search with \"%s\" extension", ext)
		}

		for _, fn := range fileList {
			picturesList = append(picturesList, fn)
		}
	}

	natsort.Strings(picturesList)

	return picturesList
}

func moveFilesAndChangePath(picturesList *[]string, newPath *string) *[]string {
	log := logging.MustGetLogger("log")
	newPathList := make([]string, len(*picturesList))

	for num, pic := range *picturesList {
		_, filename := path.Split(pic)
		np := path.Join(*newPath, filename)
		if err := os.Rename(pic, np); err != nil {
			log.Criticalf("Unable to move \"%s\" to \"%s\" !", pic, np)
			os.Exit(1)
		} else {
			log.Debugf("\"%s\" was moved to \"%s\"", pic, np)
		}

		newPathList[num] = np
	}

	return &newPathList
}

func shippingEmail(pool *email.Pool, ch chan attachment, removePic chan<- string) {
	for i := 0; i < 2; i++ {
		go func(num int, pool *email.Pool, ch chan attachment, removePic chan<- string) {
			log := logging.MustGetLogger("log")

			timeout := 10 * time.Second

			for e := range ch {
				if err := pool.Send(e.email, timeout); err != nil {
					errSplitted := strings.Split(err.Error(), " ")
					log.Debugf("Error: %s", err)

					switch errSplitted[0] {
					case "421":
						ch <- e
						log.Debug("Waiting 2 minutes since too many email have been sent !")
						time.Sleep(2 * time.Minute)
					case "550":
						log.Debug("Oops, I can't send email anymore !")
						os.Exit(1)
					case "dial":
						log.Debug("Oops, connexion refused !")
						os.Exit(1)
					}
				} else {
					log.Debug("Email was sent")

					removePic <- e.pictureName
				}
			}
		}(i, pool, ch, removePic)
	}
}

func startApp() {
	log := logging.MustGetLogger("log")

	mySMTP := viper.GetString("email.smtp")
	login := viper.GetString("email.login")
	password := viper.GetString("email.password")
	host := fmt.Sprintf("%s:%d", mySMTP, viper.GetInt("email.port"))
	sleepBeforeStart := viper.GetInt("default.sleepBeforeStarting")
	sleepBetweenLoop := viper.GetInt("default.sleepTime")
	defaultPath := viper.GetString("default.picturePath")
	newPath := path.Join(defaultPath, "send")

	removePictureChan := make(chan string, 80)
	emailChan := make(chan attachment, 80)

	createPath(&newPath)

	log.Debugf("Before starting to work, waiting \"%ds\"", sleepBeforeStart)
	time.Sleep(time.Duration(sleepBeforeStart) * time.Second)
	log.Debug("Removing all pictures before starting infinit loop")

	pool := email.NewPool(host, 4, smtp.PlainAuth("", login, password, mySMTP))

	picturesList := findPicture()
	log.Debugf("picturesList: %v", picturesList)
	removePictures(&picturesList)

	go removePictureGoroutine(removePictureChan)

	shippingEmail(pool, emailChan, removePictureChan)

	log.Debug("Starting infinit loop")
	for {
		picturesList = findPicture()
		log.Debugf("picturesList: %v", picturesList)
		newPicturesListPtr := moveFilesAndChangePath(&picturesList, &newPath)

		for _, pic := range *newPicturesListPtr {
			e := createAnEmail(&pic)
			emailChan <- attachment{
				email:       e,
				pictureName: pic,
			}
		}
		log.Debugf("I'll sleep during %d second(s)", sleepBetweenLoop)
		time.Sleep(time.Duration(sleepBetweenLoop) * time.Second)
	}
}

func removePictureGoroutine(removePic <-chan string) {
	log := logging.MustGetLogger("log")

	for pictureFilename := range removePic {
		os.Remove(pictureFilename)

		log.Debugf("Picture \"%s\" was removed", pictureFilename)
	}
}

func removePictures(picturesList *[]string) {
	log := logging.MustGetLogger("log")

	for _, pic := range *picturesList {
		os.Remove(pic)
		log.Debugf("Picture \"%s\" was removed", pic)
	}
}
