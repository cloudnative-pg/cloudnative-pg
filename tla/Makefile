# TLA+ specifications.
#
# Targets:
#   make check     re-translate every PlusCal spec and model-check all of
#                  them with TLC
#   make translate re-run the PlusCal translator on every spec
#   make clean     remove TLC output directories and translator backups
#
# Requirements are handled automatically:
#   - a Java runtime (`JAVA`, default: `java`) must be on PATH;
#   - `tla2tools.jar` defaults to the local cache and is downloaded from
#     the pinned TLA+ release when missing. Override the path and/or the
#     release (keeping TLA_SHA1 in sync when bumping the version):
#
#   make check TLA2TOOLS=/path/to/tla2tools.jar
#   make check TLA_VERSION=v1.7.4 TLA_SHA1=<sha1 of that release's jar>

TLA_VERSION ?= v1.7.4
TLA_SHA1 ?= bee4a54f3ee3d4afc347c3240ec2d9e93b075104
TLA_DOWNLOAD_URL = https://github.com/tlaplus/tlaplus/releases/download/$(TLA_VERSION)/tla2tools.jar

TLA2TOOLS ?= $(HOME)/.cache/tlaplus/$(TLA_VERSION)/tla2tools.jar
JAVA ?= java

MODELS := $(basename $(wildcard */*.tla))

.PHONY: all check translate clean check-java $(addprefix check-,$(MODELS))

# Drop a partially downloaded tla2tools.jar instead of reusing it next run.
.DELETE_ON_ERROR:

all: check

check: $(addprefix check-,$(MODELS))

$(addprefix check-,$(MODELS)): check-%: translate %.tla %.cfg
	$(JAVA) -cp "$(TLA2TOOLS)" tlc2.TLC -workers auto -config $*.cfg $*.tla

translate: check-java $(TLA2TOOLS)
	@for m in $(MODELS); do \
		echo "Translating $$m.tla"; \
		$(JAVA) -cp "$(TLA2TOOLS)" pcal.trans -nocfg $$m.tla || exit 1; \
	done

check-java:
	command -v "$(JAVA)" >/dev/null 2>&1 || { echo "error: '$(JAVA)' not found on PATH. Install a Java runtime (required to run the TLA+ tools)."; exit 1; }

$(TLA2TOOLS):
	mkdir -p "$(@D)"
	if command -v curl >/dev/null 2>&1; then \
		curl -fsSL -o "$@" "$(TLA_DOWNLOAD_URL)"; \
	else \
		echo "error: need curl to download tla2tools.jar"; exit 1; \
	fi
	if command -v sha1sum >/dev/null 2>&1; then \
		echo "$(TLA_SHA1)  $@" | sha1sum -c -; \
	elif command -v shasum >/dev/null 2>&1; then \
		echo "$(TLA_SHA1)  $@" | shasum -a 1 -c -; \
	else \
		echo "error: need sha1sum or shasum to verify tla2tools.jar"; exit 1; \
	fi

clean:
	rm -rf $(addsuffix states,$(dir $(MODELS))) $(wildcard */*.old)
