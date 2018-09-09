// Original code: https://github.com/zmb3/spotify/blob/master/examples/authenticate/authcode/authenticate.go
// This is a slightly modified version that saves off the token to s3 for re-use later.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/kelseyhightower/envconfig"
	"github.com/zmb3/spotify"
)

const redirectURI = "http://localhost:8080/callback"

var (
	// Create an authenticator with the scopes needed for these operations.
	// Spotify scopes are pretty specific, each API endpoint specifies which scopes are needed.
	auth   = spotify.NewAuthenticator(redirectURI, spotify.ScopeUserLibraryRead, spotify.ScopePlaylistModifyPrivate, spotify.ScopePlaylistReadPrivate)
	ch     = make(chan *spotify.Client)
	state  = "abc123"
	s3     *s3manager.Uploader
	config struct {
		Bucket    string `required:"true"`
		TokenFile string `required:"true"`
		Region    string `required:"true"`
	}
)

func main() {
	err := envconfig.Process("", &config)
	if err != nil {
		log.Fatal(err.Error())
	}

	s3 = s3manager.NewUploader(session.Must(session.NewSession(&aws.Config{Region: aws.String(config.Region)})))

	// Start server with a callback handler to complete the authentication.
	http.HandleFunc("/callback", completeAuth)
	go http.ListenAndServe(":8080", nil)

	url := auth.AuthURL(state)
	log.Println("Please log in to Spotify by visiting the following page in your browser:", url)

	// Wait for the authentication.
	client := <-ch

	// Get the user.
	user, err := client.CurrentUser()
	if err != nil {
		log.Fatal(err)
	}

	log.Println("You are logged in as:", user.ID)
	log.Println("Token has been saved to s3")
}

func completeAuth(w http.ResponseWriter, r *http.Request) {
	tok, err := auth.Token(state, r)
	if err != nil {
		http.Error(w, "Couldn't get token", http.StatusForbidden)
		log.Fatal(err)
	}

	if st := r.FormValue("state"); st != state {
		http.NotFound(w, r)
		log.Fatalf("State mismatch: %s != %s\n", st, state)
	}

	// Use the token to get an authenticated client.
	client := auth.NewClient(tok)
	fmt.Fprintf(w, "Login Completed!")
	ch <- &client

	btys, err := json.Marshal(tok)
	if err != nil {
		log.Fatalf("could not marshal token: %v", err)
	}

	if _, err := s3.Upload(&s3manager.UploadInput{
		Bucket: aws.String(config.Bucket),
		Key:    aws.String(config.TokenFile),
		Body:   bytes.NewReader(btys),
	}); err != nil {
		log.Fatalf("could not write token to s3: %v", err)
	}
}
