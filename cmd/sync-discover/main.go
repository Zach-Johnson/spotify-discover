package main

import (
	"bytes"
	"encoding/json"

	log "github.com/sirupsen/logrus"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/kelseyhightower/envconfig"
	"github.com/zmb3/spotify"
	"golang.org/x/oauth2"
)

var config struct {
	TargetPlaylist string `envconfig:"TARGET_PLAYLIST" required:"true"`
	Bucket         string `required:"true"`
	TokenFile      string `envconfig:"TOKEN_FILE" required:"true"`
	Region         string `required:"true"`
}

func main() {
	lambda.Start(handler)
}

func handler() {
	err := envconfig.Process("", &config)
	if err != nil {
		log.Fatal(err.Error())
	}

	sess := session.Must(session.NewSession(&aws.Config{Region: aws.String(config.Region)}))
	s3dl := s3manager.NewDownloader(sess)
	s3ul := s3manager.NewUploader(sess)

	buff := &aws.WriteAtBuffer{}
	if _, err = s3dl.Download(buff, &s3.GetObjectInput{
		Bucket: aws.String(config.Bucket),
		Key:    aws.String(config.TokenFile),
	}); err != nil {
		log.WithError(err).Fatal("failed to download token file from S3")
	}

	tok := new(oauth2.Token)
	if err := json.Unmarshal(buff.Bytes(), tok); err != nil {
		log.WithError(err).Fatal("could not unmarshal token")
	}

	client := spotify.NewAuthenticator("").NewClient(tok)

	newToken, err := client.Token()
	if err != nil {
		log.WithError(err).Fatal("could not retrieve token from client")
	}

	if newToken.AccessToken != tok.AccessToken {
		log.Info("got refreshed token, saving it")

		btys, err := json.Marshal(newToken)
		if err != nil {
			log.Fatalf("could not marshal token: %v", err)
		}

		if _, err := s3ul.Upload(&s3manager.UploadInput{
			Bucket: aws.String(config.Bucket),
			Key:    aws.String(config.TokenFile),
			Body:   bytes.NewReader(btys),
		}); err != nil {
			log.Fatalf("could not write token to s3: %v", err)
		}

	}

	user, err := client.CurrentUser()
	if err != nil {
		log.WithError(err).Fatal("could not get user")
	}

	playlists, err := client.GetPlaylistsForUser(user.ID)
	if err != nil {
		log.WithError(err).Fatal("could not get playlists")
	}

	var (
		discoverID spotify.ID
		targetID   spotify.ID
	)

	// Get the ID of the Discover Weekly playlist and the target playlist.
	for _, p := range playlists.Playlists {
		if p.Name == "Discover Weekly" {
			discoverID = p.ID
		}

		if p.Name == config.TargetPlaylist {
			targetID = p.ID
		}
	}

	if discoverID == "" || targetID == "" {
		log.Fatal("did not get playlist IDs")
	}

	// Get songs from the Discover Weekly playlist.
	discoverPlaylist, err := client.GetPlaylist(user.ID, discoverID)
	if err != nil {
		log.WithError(err).Fatal("could not get Discover Weekly playlist")
	}

	// For each song, check if it is saved in the library.
	trackIDs := make([]spotify.ID, 0, len(discoverPlaylist.Tracks.Tracks))

	for _, t := range discoverPlaylist.Tracks.Tracks {
		trackIDs = append(trackIDs, t.Track.SimpleTrack.ID)
	}

	hasTracks, err := client.UserHasTracks(trackIDs...)
	if err != nil {
		log.WithError(err).Fatal("could not check if tracks exist in library")
	}

	addTracks := make([]spotify.ID, 0, len(discoverPlaylist.Tracks.Tracks))
	for i, b := range hasTracks {
		if b {
			addTracks = append(addTracks, trackIDs[i])
		}
	}

	// Add each saved song to the specified playlist.
	_, err = client.AddTracksToPlaylist(user.ID, targetID, addTracks...)
	if err != nil {
		log.WithError(err).Fatal("could not add tracks to playlist")
	}

	log.Infof("successfully added %d tracks to playlist %s", len(addTracks), config.TargetPlaylist)
}
