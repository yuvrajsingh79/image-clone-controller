package controller

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

var repository string = os.Getenv("REPOSITORY")

// retagImage tags the image with new tag. i.e, backup-reistry-name/image-name:tag
func retagImage(name string) (string, string, string) {
	var imageName, imageNameWithTag, tag, newImage string
	image := strings.Split(name, "/")
	if len(image) == 2 {
		imageNameWithTag = image[1]
	} else {
		imageNameWithTag = image[0]
	}
	if strings.Contains(imageNameWithTag, ":") {
		list := strings.Split(imageNameWithTag, ":")
		imageName = list[0]
		tag = list[1]
	} else {
		imageName = imageNameWithTag
	}
	imageName = repository + "/" + imageName
	if len(tag) > 0 {
		newImage = imageName + ":" + tag
	} else {
		newImage = imageName
	}
	return imageName, tag, newImage
}

// imageAlreadyPresentInRepo checks if image is already there in the repo
func imageAlreadyPresentInRepo(registry, tag string, opt remote.Option) bool {
	rep, _ := name.NewRepository(registry)
	list, _ := remote.List(rep, opt)
	for _, t := range list {
		if t == tag {
			return true
		}
	}
	return false
}

// getRegistryCredentials gets username and password for given registry from env variable and returns authorization information for connecting to a Registry
func getRegistryCredentials() (authn.Authenticator, error) {
	username := os.Getenv("USERNAME")
	password := os.Getenv("PASSWORD")
	if len(username) == 0 || len(password) == 0 {
		return nil, errors.New("failed to fetch credentials")
	}
	auth := authn.AuthConfig{
		Username: username,
		Password: password,
	}
	authenticator := authn.FromConfig(auth)
	return authenticator, nil
}

// ProcessImage process public image, retags it and pushes to private registry
func processImage(imgName string) (string, error) {
	oldImageref, err := name.ParseReference(imgName)
	if err != nil {
		return "", fmt.Errorf("error while parsing old image '%s' as reference. Error: '%s'", imgName, err)
	}
	authenticator, err := getRegistryCredentials()
	if err != nil {
		return "", fmt.Errorf("error while getting private registry creadentials. Error: '%s'", err)
	}
	// override the default authenticator (i.e, authn.Anonymous) for remote operations.
	opt := remote.WithAuth(authenticator)
	img, err := remote.Image(oldImageref)
	if err != nil {
		return "", err
	}
	registry, tag, newImage := retagImage(imgName)
	newImageRef, err := name.ParseReference(newImage)
	if err != nil {
		return "", fmt.Errorf("error while parsing new image '%s' as reference. Error: '%s'", imgName, err)
	}
	if !imageAlreadyPresentInRepo(registry, tag, opt) {
		//push the newly tagged image to registry
		if err := remote.Write(newImageRef, img, opt); err != nil {
			return "", fmt.Errorf("error while pushing newly tagged image '%s' to registry. Error: '%s'", newImageRef, err)
		}
	}
	return newImage, nil
}
