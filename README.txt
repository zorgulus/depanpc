PROJET DEPAN PC
================

Systeme de depannage PC terrain : hotspot Android + agent Go sur le PC a
depanner + centre de controle sur portable, connectes en WebSocket (JSON).

REGLES NON NEGOCIABLES
-----------------------
- L'agent n'execute que des commandes de sa whitelist fermee, codee en dur
  (agent/whitelist.go) : impossible d'invoquer un nom de commande hors de
  cette liste, quel que soit le contenu envoye par le client.
- EXCEPTION ASSUMEE : la commande run_command (whitelistee comme toutes
  les autres) execute elle une chaine arbitraire dans un vrai shell
  (cmd ou PowerShell) - acces complet type SSH, y compris registre et
  profondeurs de Windows. Gardee derriere enable_shell, qui exige UNE
  confirmation manuelle par connexion (pas par commande) avant que
  run_command devienne utilisable. Voir docs/PROTOCOL.md pour le detail.
- Commandes "read" (dont run_command une fois deverrouille) : execution
  automatique, sans confirmation par appel.
- Commandes "action" (kill_process, flush_dns, enable_shell) :
  confirmation manuelle obligatoire avant execution (voir docs/PROTOCOL.md).
- Connexion chiffree (wss://, certificat ephemere) depuis la phase acces
  etendu - protege contre l'ecoute passive du token/trafic sur le hotspot.
- Verrou anti-bruteforce sur l'authentification (5 echecs -> 30s de blocage).
- Tout ce qui se passe sur l'agent est journalise localement (agent.log,
  cree a cote de l'executable).

PHASES
------
1. Proto localhost (fait) - agent + client WebSocket sur 127.0.0.1,
   4 commandes bidon (ping, get_hostname, get_uptime, dummy_action) pour
   valider le format des messages et le flux de confirmation.
2. Hotspot reel (fait) - agent ecoute sur toutes les interfaces (hotspot
   Android inclus), affiche son IP et un token de connexion a chaque
   demarrage ; authentification par token obligatoire avant toute commande.
3. Catalogue complet (fait) - vraies commandes de diagnostic (list_disks,
   network_info, list_processes, get_event_log) et deux actions reelles
   (kill_process, flush_dns), toutes soumises aux memes regles (read auto,
   action confirmee, tout logue).
4. Packaging final (fait) - depanpc-agent.exe autonome dans dist/, pret a
   copier sur cle USB et lancer par double-clic, avec sa notice
   (dist/MODE_EMPLOI.txt).

HEBERGEMENT EN LIGNE (telechargement direct)
-----------------------------------------------
Le binaire est publie sur GitHub Releases (depot public) :
  https://github.com/zorgulus/depanpc

Lien stable, ne change jamais meme apres une nouvelle publication
(pointe toujours vers la derniere version) :
  https://github.com/zorgulus/depanpc/releases/latest/download/depanpc-agent.exe

Version raccourcie, simple a taper sur le PC a depanner :
  tinyurl.com/zorgulus

Pour publier une nouvelle version apres une modification du code :
  .\publish.ps1
(compile, cree une nouvelle release GitHub et y attache l'exe ; le lien
"latest" et le lien raccourci ci-dessus continuent de fonctionner sans
etre regeneres).

Sur le PC a depanner, le fichier telecharge via navigateur porte la
"marque du web" de Windows : au premier lancement, SmartScreen affichera
un avertissement ("Windows a protege votre ordinateur"). C'est normal
pour un exe non signe telecharge depuis internet -> cliquer sur
"Plus d'infos" puis "Executer quand meme".

LANCEMENT COTE PORTABLE DE CONTROLE
--------------------------------------
Double-cliquer sur "Depanner un PC.lnk" (dans ce dossier) ou lancer
depanner.bat directement. Ca prepare l'environnement client (venv Python,
premiere fois seulement) puis lance Claude Code dans ce dossier, pret a
piloter le depannage (decouverte de l'agent, envoi de commandes,
confirmation des actions).

UTILISATION SUR LE TERRAIN
----------------------------
1. Sur le PC a depanner : soit copier dist/depanpc-agent.exe depuis une
   cle USB, soit ouvrir un navigateur et telecharger directement via
   tinyurl.com/zorgulus (necessite une connexion internet, par exemple
   via le hotspot Android une fois connecte).
2. Double-cliquer sur depanpc-agent.exe : une fenetre affiche la version,
   les IP reseau et le token de connexion.
3. Depuis le portable de controle (connecte au meme hotspot) :

     cd client
     python -m venv .venv
     .venv\Scripts\pip install -r requirements.txt
     .venv\Scripts\python client.py

   Sans --host, le client scanne automatiquement le reseau local a la
   recherche de l'agent (pas besoin de lire/taper l'IP affichee par
   l'agent - seul le token reste a saisir manuellement). --host <ip> reste
   possible pour cibler directement une adresse connue. Le client passe
   ensuite en mode interactif (kill_process {"pid": 1234} pour passer des
   parametres).

   Pour juste lister les agents detectes sans se connecter :
   python discover.py

   La decouverte (client/discovery.py) scanne les sous-reseaux IPv4 locaux
   du portable de controle et confirme chaque hit via une tentative
   d'authentification volontairement invalide (reponse "auth_failed"
   attendue) - jamais de commande envoyee avant la vraie authentification.
   Sur un portable avec plusieurs reseaux actifs (VPN, machines
   virtuelles...), le scan couvre tous les sous-reseaux detectes et peut
   remonter plusieurs resultats ; --subnet <cidr> permet de cibler
   precisement le reseau du hotspot si besoin.

DEVELOPPEMENT / REBUILD
--------------------------
  Rebuild du binaire final (dist/depanpc-agent.exe), symboles allages,
  version horodatee :
     .\build.ps1

  Build de dev rapide (depuis agent/), sans optimisation :
     cd agent
     go build -o agent.exe .

  Test automatise complet (17 verifications, y compris les actions
  reelles), a lancer en local sur la meme machine que l'agent (le token
  n'est plus lisible dans agent.log depuis la correction de l'audit
  securite -- le passer explicitement, tel qu'affiche par l'agent au
  demarrage) :
     cd client
     .venv\Scripts\python smoke_test.py --token <token affiche par l'agent>

L'agent ecoute par defaut sur 0.0.0.0:8765 : accessible aussi bien en
local (127.0.0.1) que depuis n'importe quel appareil du hotspot. Le flag
-listen <host:port> permet de changer l'adresse/port si besoin.

Voir docs/PROTOCOL.md pour le detail des messages JSON et du catalogue de
commandes.
