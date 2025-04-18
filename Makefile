run:
	HTTPS_PROXY=http://127.0.0.1:7897 LLM=gemini LLM_APIKEY=your_api_key go run main.go

irun:
	LLM=glm LLM_APIKEY=your_api_key go run main.go
build:
	env CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o plant main.go

image: build
	docker build -t xshrim/plant .