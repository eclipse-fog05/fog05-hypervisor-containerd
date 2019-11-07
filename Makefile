all:
	go build src/plugin.go src/types.go

clean:
	rm -rf plugin