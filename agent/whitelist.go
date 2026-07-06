package main

import "encoding/json"

// Catégories de commandes. "read" s'exécute immédiatement, "action" exige
// une confirmation manuelle du côté du centre de contrôle avant exécution.
const (
	CategoryRead   = "read"
	CategoryAction = "action"
)

// Handler exécute une commande et renvoie son résultat.
type Handler func(params json.RawMessage) (interface{}, error)

// CommandDef décrit une entrée de la whitelist.
type CommandDef struct {
	Category string
	Handler  Handler
}

// whitelist est LA seule source de vérité des commandes exécutables par
// l'agent. Elle est codée en dur : ajouter une commande impose de modifier
// ce fichier et de recompiler l'agent. Aucune commande arbitraire ne peut
// être exécutée depuis le centre de contrôle.
var whitelist = map[string]CommandDef{
	"ping": {
		Category: CategoryRead,
		Handler:  cmdPing,
	},
	"get_hostname": {
		Category: CategoryRead,
		Handler:  cmdGetHostname,
	},
	"get_uptime": {
		Category: CategoryRead,
		Handler:  cmdGetUptime,
	},
	"list_disks": {
		Category: CategoryRead,
		Handler:  cmdListDisks,
	},
	"network_info": {
		Category: CategoryRead,
		Handler:  cmdNetworkInfo,
	},
	"list_processes": {
		Category: CategoryRead,
		Handler:  cmdListProcesses,
	},
	"get_event_log": {
		Category: CategoryRead,
		Handler:  cmdGetEventLog,
	},
	"kill_process": {
		Category: CategoryAction,
		Handler:  cmdKillProcess,
	},
	"flush_dns": {
		Category: CategoryAction,
		Handler:  cmdFlushDNS,
	},
}
