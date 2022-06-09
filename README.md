
`dx` lists and inspect various Docker objects and has by default a much more
compact output than traditional Docker commands. It is mostly made for my own
convenience.

Installation:

```console
$ go install github.com/quite/dx@latest
```

Example output:

```console
$ dx ps
id     name   up ip         ports               cmd                                image               age
c19da4 minio1 2h 172.17.0.2 9000→9000,9001→9001 /usr/bin/docker-e…le-address :9001 quay.io/minio/minio 19h
```
