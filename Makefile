WD := $(shell dirname $(realpath $(lastword $(MAKEFILE_LIST))));
UUID = $(shell ./to_uuid.sh)

ETC_FOS_DIR = /etc/fos/
VAR_FOS_DIR = /var/fos/
FOS_CONF_FILE = /etc/fos/agent.json
CTD_PLUGIN_DIR = /etc/fos/plugins/containerd
CTD_PLUGIN_CONF = $(CTD_PLUGIN_DIR)/containerd_plugin.json
SYSTEMD_DIR = /lib/systemd/system/

all:
	go build src/plugin.go src/types.go

clean:
	rm -rf plugin


install:
ifeq "$(wildcard $(CTD_PLUGIN_DIR))" ""
	sudo mkdir $(CTD_PLUGIN_DIR)
	install -m 0755 plugin $(CTD_PLUGIN_DIR)/containerd_plugin
	sudo cp -r etc/containerd_plugin.json $(CTD_PLUGIN_DIR)/containerd_plugin.json
else
	sudo cp -r plugin $(CTD_PLUGIN_DIR)/containerd_plugin
endif
	sudo cp ./fos_ctd.service $(SYSTEMD_DIR)
	sudo sh -c "echo $(UUID) | xargs -i  jq  '.configuration.nodeid = \"{}\"' $(CTD_PLUGIN_CONFFILE) > /tmp/ctd_plugin.tmp && mv /tmp/ctd_plugin.tmp $(CTD_PLUGIN_CONFFILE)"


