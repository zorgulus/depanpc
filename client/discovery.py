"""Découverte automatique d'un agent DEPAN PC sur le réseau local.

Scanne les sous-réseaux IPv4 locaux (celui du hotspot inclus) à la
recherche d'un service qui répond au protocole de l'agent. Aucun token
n'est nécessaire pour la découverte : une tentative d'authentification
volontairement invalide suffit à obtenir un "auth_failed", qui confirme
qu'il s'agit bien de l'agent DEPAN PC (et pas d'un autre service qui
écoute sur ce port). Cette tentative est journalisée côté agent comme
n'importe quelle authentification échouée.
"""

import asyncio
import ipaddress
import json

import psutil
import websockets

DEFAULT_PORT = 8765


def candidate_networks(min_prefix_len=22):
    """Sous-réseaux IPv4 locaux plausibles (hotspot inclus), en écartant
    loopback, link-local, et les réseaux trop grands pour être scannés
    rapidement (préfixe plus large que /22, donc plus de ~1000 hôtes)."""
    nets = []
    for iface, addrs in psutil.net_if_addrs().items():
        for addr in addrs:
            if addr.family.name != "AF_INET":
                continue
            ip = addr.address
            netmask = addr.netmask
            if not netmask or ip.startswith("127.") or ip.startswith("169.254."):
                continue
            try:
                net = ipaddress.ip_network(f"{ip}/{netmask}", strict=False)
            except ValueError:
                continue
            if net.prefixlen < min_prefix_len:
                continue
            nets.append((iface, net))
    return nets


async def _probe(ip, port, timeout):
    url = f"ws://{ip}:{port}/ws"
    try:
        async with asyncio.timeout(timeout):
            async with websockets.connect(url, open_timeout=timeout) as ws:
                await ws.send(json.dumps({"id": "discover", "type": "auth", "token": ""}))
                raw = await ws.recv()
                data = json.loads(raw)
                return data.get("type") == "error" and data.get("error") == "auth_failed"
    except Exception:
        return False


async def scan(port=DEFAULT_PORT, timeout=0.3, concurrency=100, subnet=None, progress=None):
    if subnet:
        nets = [("manuel", ipaddress.ip_network(subnet, strict=False))]
    else:
        nets = candidate_networks()

    if not nets:
        return []

    sem = asyncio.Semaphore(concurrency)
    found = []
    seen = set()
    tasks = []

    async def check(ip):
        async with sem:
            if await _probe(ip, port, timeout):
                found.append(str(ip))

    for iface, net in nets:
        if progress:
            progress(iface, net)
        for host in net.hosts():
            if host in seen:
                continue
            seen.add(host)
            tasks.append(check(host))

    await asyncio.gather(*tasks)
    return found


def find_agents(port=DEFAULT_PORT, timeout=0.3, concurrency=100, subnet=None, progress=None):
    """Wrapper synchrone : renvoie la liste des IP où un agent DEPAN PC a répondu."""
    return asyncio.run(scan(port=port, timeout=timeout, concurrency=concurrency, subnet=subnet, progress=progress))
