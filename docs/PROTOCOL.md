# Protocole WebSocket — Phase 2 (hotspot)

Un seul type de message JSON transite dans les deux sens :

```json
{
  "id": "string",
  "type": "auth | request | response | confirmation_required | confirm | error",
  "command": "string (request, confirmation_required)",
  "params": {"...": "..."} (request),
  "status": "ok | error (response)",
  "result": {"...": "..."} (response, si status=ok),
  "error": "code d'erreur (response si status=error, ou error)",
  "confirm_token": "string (confirmation_required, confirm)",
  "token": "string (auth)"
}
```

## Authentification (obligatoire, tout premier message de la connexion)

Depuis la phase 2, l'agent écoute sur toutes les interfaces réseau (hotspot inclus, plus seulement localhost). Il est donc joignable par tout appareil connecté au même hotspot. Pour limiter ce risque, l'agent génère un token aléatoire à chaque démarrage, affiché sur l'écran du PC dépanné (et journalisé) :

```
client -> {"id":"0","type":"auth","token":"7c0caf455e92999a"}
agent  -> {"id":"0","type":"response","status":"ok"}
```

Si le premier message n'est pas de type `auth` ou que le token est incorrect, l'agent répond `error: auth_failed` et **ferme immédiatement la connexion**, sans jamais traiter de `request`. Chaque tentative (réussie ou non) est journalisée avec l'adresse distante.

Ce n'est pas un mécanisme de chiffrement ni une protection contre un attaquant actif sur le réseau — juste un garde-fou contre une connexion accidentelle ou curieuse sur le hotspot.

## Flux "read" (auto)

```
client -> {"id":"1","type":"request","command":"ping","params":{}}
agent  -> {"id":"1","type":"response","status":"ok","result":{"pong":true,"time":"..."}}
```

## Flux "action" (confirmation manuelle obligatoire)

```
client -> {"id":"1","type":"request","command":"dummy_action","params":{}}
agent  -> {"id":"1","type":"confirmation_required","command":"dummy_action","confirm_token":"tok-abc"}

--- confirmation manuelle de l'opérateur côté client ---

client -> {"id":"2","type":"confirm","confirm_token":"tok-abc"}
agent  -> {"id":"2","type":"response","status":"ok","result":{...}}
```

Un token expire après **60 secondes** et n'est utilisable **qu'une seule fois**. Passé ce délai, ou après consommation, toute confirmation avec ce token renvoie `error: invalid_or_expired_token`.

## Erreurs

| Code | Cas |
|---|---|
| `invalid_json` | message reçu non parsable |
| `auth_failed` | premier message absent, mal formé, ou token d'authentification incorrect |
| `unknown_message_type` | `type` autre que request/confirm (après authentification) |
| `unknown_command` | commande absente de la whitelist |
| `invalid_or_expired_token` | confirmation avec un token invalide, expiré ou déjà consommé |

## Whitelist (`agent/whitelist.go`)

| Commande | Catégorie | Params | Description |
|---|---|---|---|
| `ping` | read | — | test de connectivité |
| `get_hostname` | read | — | nom de la machine |
| `get_uptime` | read | — | uptime système réel (`GetTickCount64`) |
| `list_disks` | read | — | usage disque par volume (total/libre/utilisé) |
| `network_info` | read | — | interfaces réseau et leurs adresses |
| `list_processes` | read | — | processus en cours, triés par mémoire, plafonné à 50 |
| `get_event_log` | read | `log` (`System`\|`Application`, défaut `System`), `max` (1-50, défaut 20) | entrées Critique/Erreur/Avertissement récentes du journal d'événements |
| `kill_process` | action | `pid` (entier positif) | termine un processus (`taskkill /F`) |
| `flush_dns` | action | — | vide le cache DNS local (`ipconfig /flushdns`) |

La whitelist est codée en dur dans le binaire. Aucun mécanisme ne permet d'exécuter une commande hors de cette liste, quel que soit le contenu envoyé par le client. Les commandes qui s'appuient sur un utilitaire système (`tasklist`, `taskkill`, `ipconfig`, `wevtutil`) le font avec une ligne de commande entièrement fixée par l'agent : les seuls éléments variables (nom de journal, PID, nombre max d'entrées) sont validés/clampés avant construction des arguments, jamais concaténés dans une chaîne shell.

### Encodage des sorties console

`tasklist`, `taskkill` et `ipconfig` sortent dans la codepage OEM de la console (850 sur une install française) ; `wevtutil` sort en codepage ANSI système lorsque sa sortie est redirigée. L'agent convertit explicitement (`agent/syscalls.go`, `decodeOEM`/`decodeANSI` via `MultiByteToWideChar`) avant d'encoder en JSON, sinon les caractères accentués deviennent illisibles voire invalides en UTF-8.

## Log agent (`agent/agent.log`)

Une ligne JSON par événement : `startup` (avec token, adresse d'écoute, IP détectées), `connection_open`, `connection_close`, `auth_success`, `auth_failed`, `request_received`, `executed`, `rejected_unknown_command`, `confirmation_required`, `action_executed`, `confirm_rejected`, `execution_error`.

## Découverte de l'agent sur le hotspot

L'IP attribuée par le hotspot Android est dynamique. Au démarrage, l'agent affiche sur son propre écran (celui du PC à dépanner) toutes ses adresses IPv4 non-loopback. L'opérateur lit cette IP et le token directement sur l'écran du PC dépanné, et les saisit dans le client (`--host <ip>` et le token demandé).
