"""CLI autonome : liste les agents DEPAN PC trouvés sur le réseau local,
sans se connecter avec le client interactif. Utile pour vérifier la
découverte indépendamment, ou pour scripter autre chose autour."""

import argparse
import sys

from discovery import DEFAULT_PORT, find_agents


def main():
    parser = argparse.ArgumentParser(description="Découverte automatique de l'agent DEPAN PC sur le réseau local")
    parser.add_argument("--port", type=int, default=DEFAULT_PORT)
    parser.add_argument("--timeout", type=float, default=0.5, help="délai max par hôte testé (secondes)")
    parser.add_argument("--concurrency", type=int, default=100)
    parser.add_argument("--subnet", default=None, help="forcer un sous-réseau précis, ex: 192.168.43.0/24")
    args = parser.parse_args()

    def progress(iface, net):
        print(f"Scan de {net} (interface {iface})...", file=sys.stderr)

    found = find_agents(port=args.port, timeout=args.timeout, concurrency=args.concurrency, subnet=args.subnet, progress=progress)

    if not found:
        print("Aucun agent DEPAN PC trouvé.", file=sys.stderr)
        sys.exit(1)

    for ip in found:
        print(ip)


if __name__ == "__main__":
    main()
