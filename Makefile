.PHONY: build run tidy dev docker clean

APP_NAME=shortlink
BUILD_DIR=./build

build:
	mkdir -p $(BUILD_DIR)
	go build -buildvcs=false -o $(BUILD_DIR)/$(APP_NAME) ./cmd/server

run:
	go run ./cmd/server

tidy:
	go mod tidy

dev:
	@if [ ! -f .env ]; then echo "缺少 .env，请先运行 ./deploy.sh 初始化配置"; exit 1; fi
	docker compose -f docker-compose.yml up --build

clean:
	rm -rf $(BUILD_DIR)
