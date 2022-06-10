#!/bin/bash

go get -u -v
for i in $(ls -1 */go.mod); do
	( cd $(dirname "$i"); go get -u -v; go mod tidy -v )
done

