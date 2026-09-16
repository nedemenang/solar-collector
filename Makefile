.PHONY: tests

tests:
	go test ./...
	cd simulators/mqtt-publisher && npm test
	cd simulators/opcua-server && npm test
