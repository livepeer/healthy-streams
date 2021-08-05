version ?= $(shell git describe --tag --dirty)
cmd ?= analyzer

allCmds := $(shell ls ./cmd/)
dockerimg := livepeer/data

.PHONY: all $(allCmds) docker docker_run docker_push deps_start deps_stop check_local_rabbit

all: $(allCmds)

$(allCmds):
	$(MAKE) -C ./cmd/$@

run: check_local_rabbit deps_start
	$(MAKE) -C ./cmd/$(cmd) run

docker:
	docker build -t $(dockerimg) -t $(dockerimg):$(version) --build-arg version=$(version) .

docker_run: deps_start docker
	docker run -it --rm --name=$(cmd) --entrypoint=./$(cmd) \
		--network=livepeer-data_default -p 8080:8080 \
		-e LP_RABBITMQURI=rabbitmq-stream://rabbitmq/livepeer \
		$(dockerimg) $(args)

docker_push:
	docker push $(dockerimg):$(version)
	docker push $(dockerimg):latest

deps_start:
	docker-compose up -d
	@printf 'Waiting rabbitmq healthy ...'
	@until docker inspect --format='{{json .State.Health.Status}}' rabbitmq | grep -q '"healthy"' ; \
	do \
		printf '.' && sleep 1 ; \
	done;
	@printf " \e[32mdone\e[m\n"


deps_stop:
	docker-compose down

check_local_rabbit:
	@cat /etc/hosts | grep -wq rabbitmq || { \
		echo "Host for local rabbitmq not configured in /etc/hosts."; \
		echo "To run locally, configure it with:"; \
		echo "> echo '127.0.0.1 rabbitmq' >> /etc/hosts"; \
		exit 1; \
	}
