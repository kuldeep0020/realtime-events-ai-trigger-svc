.PHONY: build test image push migrate seed serve demo-fire-realestate demo-fire-rs-self

IMAGE := hub.dev-rudder.rudderlabs.com/kuldeep/realtime-ai-trigger-svc:0.1.0
BINARY := ./realtime-trigger

build:
	go build -o $(BINARY) ./cmd/realtime-trigger

test:
	go test ./...

image:
	docker build -t $(IMAGE) .

push:
	docker push $(IMAGE)

migrate: build
	$(BINARY) migrate

seed: build
	$(BINARY) seed --from hand

serve: build
	$(BINARY) serve

demo-fire-realestate: build
	$(BINARY) demo-fire --persona realestate

demo-fire-rs-self: build
	$(BINARY) demo-fire --persona rs-self
