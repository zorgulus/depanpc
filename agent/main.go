package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	confirmTTL     = 60 * time.Second
	logFile        = "agent.log"
	maxMessageSize = 16 * 1024 // largement suffisant pour nos messages JSON
)

// buildVersion est injecté au build via -ldflags -X (voir build.ps1).
// Utile pour savoir, sur le terrain, quelle version tourne réellement.
var buildVersion = "dev"

var upgrader = websocket.Upgrader{
	// Phase 1 : localhost uniquement, pas de vérification d'origine.
	// Sera durci en phase 2 (hotspot).
	CheckOrigin: func(r *http.Request) bool { return true },
}

type pendingAction struct {
	Command string
	Params  json.RawMessage
	Expiry  time.Time
}

func main() {
	listenAddr := flag.String("listen", "0.0.0.0:8765", "adresse d'écoute (host:port)")
	flag.Parse()

	// On tente d'écouter AVANT d'afficher/journaliser quoi que ce soit : si
	// un agent tourne déjà sur ce PC, on l'apprend immédiatement au lieu
	// d'afficher une bannière trompeuse (nouveau token, IP...) pour une
	// instance qui va mourir aussitôt.
	listener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Impossible de démarrer l'agent sur %s : %v\n", *listenAddr, err)
		fmt.Fprintln(os.Stderr, "Un agent est probablement déjà en cours d'exécution sur ce PC (vérifier les fenêtres ouvertes).")
		os.Exit(1)
	}

	logger, err := NewLogger(logFile)
	if err != nil {
		log.Fatalf("impossible d'ouvrir le log: %v", err)
	}
	defer logger.Close()

	authToken := newToken()
	ips := localIPv4Addrs()

	fmt.Println("=== Agent DEPAN PC ===")
	fmt.Printf("Version : %s\n", buildVersion)
	fmt.Printf("Écoute sur ws://%s/ws\n", *listenAddr)
	if len(ips) > 0 {
		fmt.Printf("Adresses IP possibles pour le client : %s\n", strings.Join(ips, ", "))
	} else {
		fmt.Println("Aucune adresse IP réseau détectée (localhost uniquement).")
	}
	fmt.Printf("Token de connexion (à saisir dans le client) : %s\n", authToken)
	fmt.Println("Cette fenêtre doit rester ouverte tant que le dépannage est en cours.")
	fmt.Println("Fermer la fenêtre (ou Ctrl+C) arrête l'agent.")
	fmt.Println("======================")

	// Le token n'est jamais écrit en clair dans le log : agent.log est un
	// fichier local lisible par tout processus ayant accès au disque, et le
	// token y transiterait sinon en clair (voir maskToken).
	logger.Log("startup", "", map[string]interface{}{
		"version": buildVersion,
		"listen":  *listenAddr,
		"ips":     ips,
		"token":   maskToken(authToken),
	})

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleConn(w, r, logger, authToken)
	})

	if err := http.Serve(listener, nil); err != nil {
		log.Fatalf("serveur arrêté: %v", err)
	}
}

// localIPv4Addrs renvoie les adresses IPv4 non-loopback de la machine, pour
// affichage à l'opérateur (l'IP réelle sur le hotspot est attribuée
// dynamiquement, impossible à connaître à l'avance).
func localIPv4Addrs() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() {
			continue
		}
		ip4 := ipNet.IP.To4()
		if ip4 == nil {
			continue
		}
		ips = append(ips, ip4.String())
	}
	return ips
}

func handleConn(w http.ResponseWriter, r *http.Request, logger *Logger, authToken string) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade websocket échoué: %v", err)
		return
	}
	defer conn.Close()
	conn.SetReadLimit(maxMessageSize)

	remote := r.RemoteAddr
	logger.Log("connection_open", "", map[string]string{"remote": remote})
	defer logger.Log("connection_close", "", map[string]string{"remote": remote})

	if !authenticate(conn, logger, remote, authToken) {
		return
	}

	pending := map[string]pendingAction{}

	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			logger.Log("bad_json", "", map[string]string{"raw": string(raw)})
			conn.WriteJSON(Message{Type: MsgError, Error: "invalid_json"})
			continue
		}

		switch msg.Type {
		case MsgRequest:
			handleRequest(conn, logger, pending, msg)
		case MsgConfirm:
			handleConfirm(conn, logger, pending, msg)
		default:
			logger.Log("unknown_message_type", "", map[string]string{"type": msg.Type})
			conn.WriteJSON(Message{ID: msg.ID, Type: MsgError, Error: "unknown_message_type"})
		}
	}
}

// authenticate exige que le tout premier message d'une connexion soit un
// message "auth" portant le token affiché au démarrage de l'agent. Toute
// connexion qui échoue à s'authentifier est immédiatement fermée et
// journalisée — l'agent étant désormais joignable depuis tout le réseau du
// hotspot et non plus seulement localhost.
func authenticate(conn *websocket.Conn, logger *Logger, remote, authToken string) bool {
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return false
	}

	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		logger.Log("auth_failed", "", map[string]string{"remote": remote, "reason": "invalid_json"})
		conn.WriteJSON(Message{Type: MsgError, Error: "invalid_json"})
		return false
	}

	tokenMatch := subtle.ConstantTimeCompare([]byte(msg.Token), []byte(authToken)) == 1
	if msg.Type != MsgAuth || !tokenMatch {
		logger.Log("auth_failed", "", map[string]string{"remote": remote, "reason": "bad_token"})
		conn.WriteJSON(Message{ID: msg.ID, Type: MsgError, Error: "auth_failed"})
		return false
	}

	logger.Log("auth_success", "", map[string]string{"remote": remote})
	conn.WriteJSON(Message{ID: msg.ID, Type: MsgResponse, Status: "ok"})
	return true
}

func handleRequest(conn *websocket.Conn, logger *Logger, pending map[string]pendingAction, msg Message) {
	def, ok := whitelist[msg.Command]
	if !ok {
		logger.Log("rejected_unknown_command", msg.Command, nil)
		conn.WriteJSON(Message{ID: msg.ID, Type: MsgError, Error: "unknown_command"})
		return
	}

	logger.Log("request_received", msg.Command, map[string]string{"category": def.Category})

	if def.Category == CategoryRead {
		result, err := def.Handler(msg.Params)
		if err != nil {
			logger.Log("execution_error", msg.Command, map[string]string{"error": err.Error()})
			conn.WriteJSON(Message{ID: msg.ID, Type: MsgResponse, Status: "error", Error: err.Error()})
			return
		}
		logger.Log("executed", msg.Command, result)
		conn.WriteJSON(Message{ID: msg.ID, Type: MsgResponse, Status: "ok", Result: result})
		return
	}

	// Catégorie "action" : jamais d'exécution immédiate, on exige une
	// confirmation manuelle explicite du centre de contrôle.
	token := newToken()
	pending[token] = pendingAction{
		Command: msg.Command,
		Params:  msg.Params,
		Expiry:  time.Now().Add(confirmTTL),
	}
	logger.Log("confirmation_required", msg.Command, map[string]string{"token": maskToken(token)})
	conn.WriteJSON(Message{ID: msg.ID, Type: MsgConfirmationRequired, Command: msg.Command, ConfirmToken: token})
}

func handleConfirm(conn *websocket.Conn, logger *Logger, pending map[string]pendingAction, msg Message) {
	action, ok := pending[msg.ConfirmToken]
	if !ok {
		logger.Log("confirm_rejected", "", map[string]string{"reason": "unknown_token"})
		conn.WriteJSON(Message{ID: msg.ID, Type: MsgError, Error: "invalid_or_expired_token"})
		return
	}
	delete(pending, msg.ConfirmToken)

	if time.Now().After(action.Expiry) {
		logger.Log("confirm_rejected", action.Command, map[string]string{"reason": "expired_token"})
		conn.WriteJSON(Message{ID: msg.ID, Type: MsgError, Error: "invalid_or_expired_token"})
		return
	}

	def, ok := whitelist[action.Command]
	if !ok {
		// Ne devrait jamais arriver (la commande était whitelistée au moment
		// de la requête), mais on refuse par sécurité plutôt que de deviner.
		logger.Log("confirm_rejected", action.Command, map[string]string{"reason": "command_no_longer_whitelisted"})
		conn.WriteJSON(Message{ID: msg.ID, Type: MsgError, Error: "unknown_command"})
		return
	}

	result, err := def.Handler(action.Params)
	if err != nil {
		logger.Log("execution_error", action.Command, map[string]string{"error": err.Error()})
		conn.WriteJSON(Message{ID: msg.ID, Type: MsgResponse, Status: "error", Error: err.Error()})
		return
	}
	logger.Log("action_executed", action.Command, result)
	conn.WriteJSON(Message{ID: msg.ID, Type: MsgResponse, Status: "ok", Result: result})
}

func newToken() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// maskToken ne garde que les 4 premiers caractères d'un token pour les
// besoins de traçabilité dans les logs, sans jamais y écrire la valeur
// complète (agent.log est un fichier local en clair, potentiellement
// lisible par un tiers sur un PC déjà compromis).
func maskToken(t string) string {
	if len(t) <= 4 {
		return "****"
	}
	return t[:4] + "..."
}
