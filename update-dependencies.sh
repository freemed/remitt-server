#!/bin/bash

go get -u -v
for i in $(ls -1 */go.mod); do
	echo "Processing $i"
	( cd $(dirname "$i"); rm go.sum -f ; go get -u -v; go mod tidy -v -go=1.22 )
done

