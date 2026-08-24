"""Contexte SSL partagé pour les connexions wss:// vers l'agent.

L'agent génère un certificat auto-signé éphémère à chaque démarrage (jamais
écrit sur disque, régénéré à chaque lancement comme le token) : il n'y a
donc pas d'autorité de confiance à valider ni de certificat stable à épingler
pour l'instant. Ce contexte désactive la vérification de certificat.

Ça reste un vrai gain de sécurité par rapport à ws:// (empêche l'écoute
passive du token et du trafic sur le hotspot), mais ne protège pas contre un
attaquant actif capable d'usurper la connexion - même limite déjà documentée
pour le token lui-même (voir docs/PROTOCOL.md).
"""

import ssl


def insecure_ssl_context() -> ssl.SSLContext:
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE
    return ctx
