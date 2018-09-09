package main

import (
	"bytes"
	"encoding/json"
	"log"

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
		log.Fatalf("failed to download token file from S3: %v", err)
	}

	tok := new(oauth2.Token)
	if err := json.Unmarshal(buff.Bytes(), tok); err != nil {
		log.Fatalf("could not unmarshal token: %v", err)
	}

	// Create a Spotify authenticator with the oauth2 token.
	// If the token is expired, the oauth2 package will automatically refresh
	// so the new token is checked against the old one to see if it should be updated.
	client := spotify.NewAuthenticator("").NewClient(tok)

	newToken, err := client.Token()
	if err != nil {
		log.Fatalf("could not retrieve token from client: %v", err)
	}

	if newToken.AccessToken != tok.AccessToken {
		log.Println("got refreshed token, saving it")

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
		log.Fatalf("could not get user: %v", err)
	}

	// Retrieve the first 50 playlists for the user.
	// @TODO update this to handle the paging in case there are more than 50 playlists.
	playlists, err := client.GetPlaylistsForUser(user.ID)
	if err != nil {
		log.Fatalf("could not get playlists: %v", err)
	}

	// Vars used to designate the Discover Weekly playlist and the target playlist.
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

	// Bail out if one of the playlists wasn't found.
	if discoverID == "" || targetID == "" {
		log.Fatal("did not get playlist IDs")
	}

	// Get songs from the Discover Weekly playlist.
	// Don't need to worry about API limits here, since it always has 30 songs in it.
	discoverPlaylist, err := client.GetPlaylist(user.ID, discoverID)
	if err != nil {
		log.Fatalf("could not get Discover Weekly playlist: %v", err)
	}

	// For each song in Discover Weekly, check if it is saved in the library.
	trackIDs := make([]spotify.ID, 0, len(discoverPlaylist.Tracks.Tracks))

	// Extract the Track IDs for each song.
	for _, t := range discoverPlaylist.Tracks.Tracks {
		trackIDs = append(trackIDs, t.Track.SimpleTrack.ID)
	}

	// Check if they are in the library.
	hasTracks, err := client.UserHasTracks(trackIDs...)
	if err != nil {
		log.Fatalf("could not check if tracks exist in library: %v", err)
	}

	// Check which tracks came back in the library and mark them as songs to be added.
	addTracks := make([]spotify.ID, 0, len(discoverPlaylist.Tracks.Tracks))
	for i, b := range hasTracks {
		if b {
			addTracks = append(addTracks, trackIDs[i])
		}
	}

	// Add each song that needs to be added to the taret playlist.
	// @TODO check for duplicates before adding them to the playlist.
	_, err = client.AddTracksToPlaylist(user.ID, targetID, addTracks...)
	if err != nil {
		log.Fatalf("could not add tracks to playlist: %v", err)
	}

	// @TODO ship this off to SNS perhaps for an email summary.
	log.Printf("successfully added %d tracks to playlist %s\n", len(addTracks), config.TargetPlaylist)
}
