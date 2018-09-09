include .env

token:
	go run cmd/get-token/main.go

update:
	env GOOS=linux go build ./cmd/sync-discover
	zip sync-discover.zip ./sync-discover
	aws lambda update-function-code \
		--region us-east-1 \
		--function-name sync-discover \
		--zip-file fileb://sync-discover.zip
	rm -f sync-discover sync-discover.zip

update-env: 
	export `cat .env | xargs`
	aws lambda update-function-configuration \
		--region us-east-1 \
		--function-name sync-discover \
		--environment Variables="{SPOTIFY_ID=${SPOTIFY_ID},SPOTIFY_SECRET=${SPOTIFY_SECRET},TARGET_PLAYLIST=${TARGET_PLAYLIST},BUCKET=${BUCKET},TOKEN_FILE=${TOKEN_FILE},REGION=${REGION}}"

upload:
	export `cat .env | xargs`
	env GOOS=linux go build ./cmd/sync-discover
	zip sync-discover.zip ./sync-discover
	aws lambda create-function \
	  	--region us-east-1 \
		--function-name sync-discover \
	  	--memory 128 \
	  	--role arn:aws:iam::526123814436:role/lambda_execution \
	  	--runtime go1.x \
	  	--zip-file fileb://sync-discover.zip \
	  	--handler sync-discover \
		--environment Variables="{SPOTIFY_ID=${SPOTIFY_ID},SPOTIFY_SECRET=${SPOTIFY_SECRET},TARGET_PLAYLIST=${TARGET_PLAYLIST},BUCKET=${BUCKET},TOKEN_FILE=${TOKEN_FILE},REGION=${REGION}}"
	rm -f sync-discover sync-discover.zip
