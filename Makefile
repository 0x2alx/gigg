# Local helpers (see dev/README.md for the container commands).
SRC  := $(CURDIR)
# where `make sync` puts a clean copy of the source (without .git, local deps or build output), for
# containers that mount it read-only
DEST ?= $(CURDIR)/out/src

.PHONY: sync
sync:
	mkdir -p $(DEST)
	rsync -a --delete --exclude .git --exclude .claude --exclude .mcp.json --exclude node_modules \
	  --exclude /out --exclude /scratch --include 'web/dist/.keep' --exclude 'web/dist/*' $(SRC)/ $(DEST)/
	# node_modules may belong to a container's user (npm ci ran there): leave it alone
	find $(DEST) -path $(DEST)/web/node_modules -prune -o -exec chmod a+rX {} +
