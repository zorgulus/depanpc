"""Client WebSocket interactif pour le centre de contrôle (phase 2 : hotspot).

Envoie des requêtes à l'agent, affiche les réponses brutes, et gère la
confirmation manuelle obligatoire pour les commandes de catégorie "action".
Le client ne connaît pas la whitelist : c'est l'agent qui décide seul de ce
qui est exécutable ou non.

Depuis la phase 2, l'agent exige une authentification par token en tout
premier message (affiché sur l'écran du PC dépanné au démarrage de l'agent).
"""

import argparse
import asyncio
import getpass
import json
import sys
import uuid

import websockets

import discovery


async def send_and_receive(ws, message):
    await ws.send(json.dumps(message))
    raw = await ws.recv()
    return json.loads(raw)


async def authenticate(ws, token):
    request = {"id": str(uuid.uuid4()), "type": "auth", "token": token}
    response = await send_and_receive(ws, request)
    if response.get("type") == "response" and response.get("status") == "ok":
        return True
    print(f"Authentification refusée par l'agent : {response.get('error', response)}")
    return False


async def run(host, port, token):
    url = f"ws://{host}:{port}/ws"
    print(f"Connexion à {url} ...")
    async with websockets.connect(url) as ws:
        if not await authenticate(ws, token):
            return

        print("Authentifié. Commandes disponibles : ping, get_hostname, get_uptime, list_disks, network_info, list_processes, get_event_log, kill_process, flush_dns.")
        print("Paramètres optionnels en JSON après la commande, ex: kill_process {\"pid\": 1234}")
        print("Tape 'quit' pour sortir.")
        while True:
            line = input("> ").strip()
            if line in ("quit", "exit"):
                break
            if not line:
                continue

            command, _, raw_params = line.partition(" ")
            params = {}
            if raw_params.strip():
                try:
                    params = json.loads(raw_params)
                except json.JSONDecodeError as exc:
                    print(f"Params JSON invalides : {exc}")
                    continue

            request = {"id": str(uuid.uuid4()), "type": "request", "command": command, "params": params}
            response = await send_and_receive(ws, request)
            print(json.dumps(response, indent=2, ensure_ascii=False))

            if response.get("type") == "confirmation_required":
                confirm_token = response["confirm_token"]
                answer = input(f"Confirmer l'exécution de '{response['command']}' ? [oui/non] ").strip().lower()
                if answer in ("oui", "o", "y", "yes"):
                    confirm = {"id": str(uuid.uuid4()), "type": "confirm", "confirm_token": confirm_token}
                    result = await send_and_receive(ws, confirm)
                    print(json.dumps(result, indent=2, ensure_ascii=False))
                else:
                    print("Annulé côté opérateur (le token expirera de lui-même côté agent après 60s).")


def resolve_host(args):
    if args.host and not args.discover:
        return args.host

    print("Recherche de l'agent sur le réseau local...")
    found = discovery.find_agents(
        port=args.port,
        progress=lambda iface, net: print(f"  scan {net} ({iface})..."),
    )

    if not found:
        print("Aucun agent trouvé sur le réseau. Précise l'adresse avec --host.")
        sys.exit(1)

    if len(found) == 1:
        print(f"Agent trouvé : {found[0]}")
        return found[0]

    print("Plusieurs agents trouvés :")
    for i, ip in enumerate(found, 1):
        print(f"  {i}. {ip}")
    choice = input("Lequel utiliser ? [numéro] ").strip()
    try:
        return found[int(choice) - 1]
    except (ValueError, IndexError):
        print("Choix invalide.")
        sys.exit(1)


def main():
    parser = argparse.ArgumentParser(description="Client de contrôle DEPAN PC")
    parser.add_argument("--host", default=None, help="IP de l'agent (si omis, recherche automatique sur le réseau local)")
    parser.add_argument("--port", default=8765, type=int, help="port de l'agent")
    parser.add_argument("--token", default=None, help="token de connexion affiché par l'agent au démarrage")
    parser.add_argument("--discover", action="store_true", help="forcer la recherche automatique même si --host est fourni")
    args = parser.parse_args()

    host = resolve_host(args)
    token = args.token or getpass.getpass("Token de connexion (affiché sur l'écran de l'agent) : ")

    try:
        asyncio.run(run(host, args.port, token))
    except (KeyboardInterrupt, EOFError):
        print("\nFermeture.")
        sys.exit(0)
    except OSError as exc:
        print(f"Impossible de joindre l'agent sur {host}:{args.port} : {exc}")
        sys.exit(1)


if __name__ == "__main__":
    main()
