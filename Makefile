GO ?= go
# The fbs checkout next to this one (the pilot runs from it).
FBS_DIR ?= ../securebrand-microservices
SPIKE = $(GO) run ./cmd/gfs-spike
SPIKE_ARGS ?=

.PHONY: all build test race lint check secret fbs-key spike-checks spike-apis spike-google clean

all: check

build:
	$(GO) build ./...

test:
	$(GO) test -count=1 ./...

race:
	$(GO) test -race -count=1 ./...

lint:
	golangci-lint run ./...

## What CI runs.
check: build test race lint

## Saves a credential into secrets/NAME (mode 600) without showing it:
##   make secret NAME=brave_api_key
## Paste the key at the prompt (it isn't echoed), then press Enter.
secret:
	@test -n "$(NAME)" || { echo "usage: make secret NAME=brave_api_key"; exit 2; }
	@case "$(NAME)" in *[!a-z0-9_]*) echo "NAME may hold only a-z, 0-9 and _"; exit 2;; esac
	@umask 077; mkdir -p secrets; chmod 700 secrets; \
	printf 'Paste the value for %s, then press Enter (nothing is shown): ' "$(NAME)"; \
	trap 'stty echo 2>/dev/null' EXIT INT TERM; stty -echo 2>/dev/null; read -r v; stty echo 2>/dev/null; echo; \
	test -n "$$v" || { echo "nothing saved: the value was empty"; exit 1; }; \
	printf '%s\n' "$$v" > secrets/$(NAME); chmod 600 secrets/$(NAME); \
	echo "saved secrets/$(NAME)"

FBS_KEY_NAME ?= google-search
FBS_KEY_SCOPES ?= data:read
## Creates an fbs API key with fbctl (the pilot must be up) and saves it
## into secrets/fbs_api_key without showing it.
fbs-key:
	@test -d $(FBS_DIR) || { echo "no fbs checkout at $(FBS_DIR) (set FBS_DIR=...)"; exit 2; }
	@umask 077; mkdir -p secrets; chmod 700 secrets; \
	key=$$(cd $(FBS_DIR) && docker compose -f deploy/compose/docker-compose.yml -f deploy/compose/pilot.yml \
	  run --rm --no-deps -T --entrypoint /usr/local/bin/fbctl api apikey create --name $(FBS_KEY_NAME) --scopes $(FBS_KEY_SCOPES) \
	  | sed -n 's/.*"api_key": *"\([^"]*\)".*/\1/p'); \
	test -n "$$key" || { echo "could not create an API key (is the pilot up?)"; exit 1; }; \
	printf '%s\n' "$$key" > secrets/fbs_api_key; chmod 600 secrets/fbs_api_key; \
	echo "saved secrets/fbs_api_key (scopes: $(FBS_KEY_SCOPES))"

## The spike (docs/spike.md).
spike-checks:
	$(SPIKE) checks $(SPIKE_ARGS)

spike-apis:
	$(SPIKE) apis $(SPIKE_ARGS)

## Google's terms don't allow automated queries: read docs/spike.md first.
spike-google:
	$(SPIKE) google --accept-google-terms-risk $(SPIKE_ARGS)

clean:
	rm -rf out
