"""Test automatisé non-interactif du catalogue phase 3 : authentification,
commandes de diagnostic réelles (disque, réseau, processus, event log), et
les deux actions réelles (flush_dns, kill_process) via le flux de
confirmation obligatoire. kill_process est testé sur un processus jetable
(ping) lancé par ce script, jamais sur un processus réel de la machine.

Le token doit être passé en argument (--token), lu sur l'écran/la sortie
de l'agent au démarrage : depuis la correction de l'audit sécurité,
agent.log ne contient plus le token en clair (seulement ses 4 premiers
caractères), donc il n'est plus possible de l'extraire automatiquement du
fichier de log.
"""

import argparse
import asyncio
import json
import subprocess
import sys
import time
import uuid

import websockets


async def send_and_receive(ws, message):
    await ws.send(json.dumps(message))
    raw = await ws.recv()
    return json.loads(raw)


async def expect(condition, description):
    status = "OK" if condition else "FAIL"
    print(f"[{status}] {description}")
    return condition


async def run(host, port, token):
    all_ok = True
    url = f"ws://{host}:{port}/ws"
    async with websockets.connect(url) as ws:
        # Authentification
        resp = await send_and_receive(ws, {"id": "auth", "type": "auth", "token": token})
        all_ok &= await expect(resp.get("type") == "response" and resp.get("status") == "ok", "authentification acceptée")

        # Commandes read historiques
        resp = await send_and_receive(ws, {"id": "1", "type": "request", "command": "ping", "params": {}})
        all_ok &= await expect(resp.get("status") == "ok" and resp.get("result", {}).get("pong") is True, "ping OK")

        resp = await send_and_receive(ws, {"id": "2", "type": "request", "command": "get_hostname", "params": {}})
        all_ok &= await expect(resp.get("status") == "ok" and "hostname" in resp.get("result", {}), "get_hostname OK")

        resp = await send_and_receive(ws, {"id": "3", "type": "request", "command": "get_uptime", "params": {}})
        all_ok &= await expect(resp.get("status") == "ok" and resp.get("result", {}).get("uptime_seconds", 0) > 0, "get_uptime renvoie une valeur réelle > 0")

        # Nouveau catalogue read
        resp = await send_and_receive(ws, {"id": "4", "type": "request", "command": "list_disks", "params": {}})
        disks = resp.get("result", {}).get("disks", [])
        all_ok &= await expect(resp.get("status") == "ok" and len(disks) > 0 and "used_percent" in disks[0], "list_disks renvoie au moins un volume")

        resp = await send_and_receive(ws, {"id": "5", "type": "request", "command": "network_info", "params": {}})
        ifaces = resp.get("result", {}).get("interfaces", [])
        all_ok &= await expect(resp.get("status") == "ok" and len(ifaces) > 0, "network_info renvoie au moins une interface")

        resp = await send_and_receive(ws, {"id": "6", "type": "request", "command": "list_processes", "params": {}})
        procs = resp.get("result", {}).get("processes", [])
        all_ok &= await expect(resp.get("status") == "ok" and len(procs) > 0 and "pid" in procs[0], "list_processes renvoie des processus")

        resp = await send_and_receive(ws, {"id": "7", "type": "request", "command": "get_event_log", "params": {"log": "System", "max": 5}})
        all_ok &= await expect(resp.get("status") == "ok" and "events" in resp.get("result", {}), "get_event_log renvoie une liste d'événements")

        resp = await send_and_receive(ws, {"id": "8", "type": "request", "command": "get_event_log", "params": {"log": "Autre"}})
        all_ok &= await expect(resp.get("status") == "error", "get_event_log refuse un nom de journal hors liste fermée")

        # Commande hors whitelist
        resp = await send_and_receive(ws, {"id": "9", "type": "request", "command": "rm_rf_everything", "params": {}})
        all_ok &= await expect(resp.get("type") == "error" and resp.get("error") == "unknown_command", "commande hors whitelist rejetée")

        # Action réelle sans risque : flush_dns, via le flux de confirmation
        resp = await send_and_receive(ws, {"id": "10", "type": "request", "command": "flush_dns", "params": {}})
        all_ok &= await expect(resp.get("type") == "confirmation_required", "flush_dns exige une confirmation")
        confirm_token = resp.get("confirm_token")
        resp = await send_and_receive(ws, {"id": "11", "type": "confirm", "confirm_token": confirm_token})
        all_ok &= await expect(resp.get("status") == "ok", "flush_dns confirmé s'exécute réellement")

        # Action réelle risquée : kill_process, testée sur un processus jetable
        dummy = subprocess.Popen(
            ["ping", "-n", "60", "127.0.0.1"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        time.sleep(0.5)
        all_ok &= await expect(dummy.poll() is None, "processus jetable (ping) bien démarré avant le test kill_process")

        resp = await send_and_receive(ws, {"id": "12", "type": "request", "command": "kill_process", "params": {"pid": dummy.pid}})
        all_ok &= await expect(resp.get("type") == "confirmation_required", "kill_process exige une confirmation")
        confirm_token = resp.get("confirm_token")
        resp = await send_and_receive(ws, {"id": "13", "type": "confirm", "confirm_token": confirm_token})
        all_ok &= await expect(resp.get("status") == "ok", "kill_process confirmé renvoie un statut ok")

        time.sleep(0.5)
        all_ok &= await expect(dummy.poll() is not None, "le processus jetable est réellement terminé après confirmation")

    # Connexion séparée avec un mauvais token d'auth -> doit être coupée
    async with websockets.connect(url) as ws2:
        resp = await send_and_receive(ws2, {"id": "14", "type": "auth", "token": "mauvais_token"})
        all_ok &= await expect(resp.get("type") == "error" and resp.get("error") == "auth_failed", "authentification avec un mauvais token refusée")

    print("\nRésultat global :", "SUCCÈS" if all_ok else "ÉCHEC")
    sys.exit(0 if all_ok else 1)


def main():
    parser = argparse.ArgumentParser(description="Smoke test DEPAN PC (catalogue phase 3)")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", default=8765, type=int)
    parser.add_argument("--token", required=True, help="token affiché par l'agent au démarrage")
    args = parser.parse_args()

    asyncio.run(run(args.host, args.port, args.token))


if __name__ == "__main__":
    main()
