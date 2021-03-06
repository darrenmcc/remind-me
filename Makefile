PROJ := darren-prd
SERVICE = remindme

build: 
	CGO_ENABLED=0 GOOS=linux go build -o ./app/server ./app

deploy: build
	# KO_DOCKER_REPO=gcr.io/$(PROJ)/$(SERVICE) ko publish ./app
	docker build --tag gcr.io/$(PROJ)/$(SERVICE) ./app/. 
	docker push gcr.io/$(PROJ)/$(SERVICE)
	gcloud run deploy $(SERVICE) --image=gcr.io/$(PROJ)/$(SERVICE) --project=$(PROJ) --platform=managed
	@rm -rf app/server

.PHONY: build deploy 
