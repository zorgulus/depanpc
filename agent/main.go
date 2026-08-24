package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	confirmTTL     = 60 * time.Second
	logFile        = "agent.log"
	maxMessageSize = 16 * 1024 // largement suffisant pour nos messages JSON

	// Anti-bruteforce sur l'authentification : au-delà de ce nombre
	// d'échecs consécutifs (toutes connexions confondues, l'agent tourne
	// seul par PC dépanné), l'agent refuse toute nouvelle tentative pendant
	// authLockoutDuration. Défense en profondeur : le token lui-même (64
	// bits d'entropie, voir newToken) rend déjà le bruteforce pratiquement
	// infaisable, mais ça protège aussi contre un futur affaiblissement du
	// format de token ou un bug d'implémentation.
	maxAuthFailures     = 5
	authLockoutDuration = 30 * time.Second
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

// authLimiter applique un verrou temporaire après plusieurs échecs
// d'authentification consécutifs. Partagé entre toutes les connexions
// (l'agent tourne seul par PC dépanné, un compteur global suffit).
type authLimiter struct {
	mu          sync.Mutex
	failures    int
	lockedUntil time.Time
}

func (l *authLimiter) allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return time.Now().After(l.lockedUntil)
}

func (l *authLimiter) recordFailure() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures++
	if l.failures >= maxAuthFailures {
		l.lockedUntil = time.Now().Add(authLockoutDuration)
		l.failures = 0
	}
}

func (l *authLimiter) recordSuccess() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures = 0
}

// generateSelfSignedCert crée un certificat TLS éphémère (en mémoire
// uniquement, jamais écrit sur disque, régénéré à chaque démarrage comme le
// token). Objectif : empêcher l'écoute passive du token et du trafic sur le
// hotspot (WireShark etc.), pas authentifier l'identité du PC dépanné face à
// un attaquant actif capable d'usurper la connexion — même limite déjà
// documentée pour le token lui-même (voir docs/PROTOCOL.md).
func generateSelfSignedCert() (tls.Certificate, string, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("génération de clé: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("génération de numéro de série: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "DepanPC Agent (ephemere)"},
		NotBefore:    time.Now().Add(-5 * time.Minute),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, "", fmt.Errorf("création du certificat: %w", err)
	}

	fingerprint := sha256.Sum256(derBytes)

	cert := tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}
	return cert, hex.EncodeToString(fingerprint[:]), nil
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

	cert, fingerprint, err := generateSelfSignedCert()
	if err != nil {
		log.Fatalf("impossible de générer le certificat TLS: %v", err)
	}
	tlsListener := tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{cert}})

	authToken := newHumanToken()
	limiter := &authLimiter{}
	ips := localIPv4Addrs()

	fmt.Println("=== Agent DEPAN PC ===")
	fmt.Printf("Version : %s\n", buildVersion)
	fmt.Printf("Écoute sur wss://%s/ws (connexion chiffrée)\n", *listenAddr)
	if len(ips) > 0 {
		fmt.Printf("Adresses IP possibles pour le client : %s\n", strings.Join(ips, ", "))
	} else {
		fmt.Println("Aucune adresse IP réseau détectée (localhost uniquement).")
	}
	fmt.Printf("Token de connexion (à saisir dans le client) : %s\n", authToken)
	fmt.Printf("Empreinte du certificat (optionnel, à comparer si besoin) : %s\n", fingerprint)
	fmt.Println("Cette fenêtre doit rester ouverte tant que le dépannage est en cours.")
	fmt.Println("Fermer la fenêtre (ou Ctrl+C) arrête l'agent.")
	fmt.Println("======================")

	// Le token n'est jamais écrit en clair dans le log : agent.log est un
	// fichier local lisible par tout processus ayant accès au disque, et le
	// token y transiterait sinon en clair (voir maskToken).
	logger.Log("startup", "", map[string]interface{}{
		"version":              buildVersion,
		"listen":               *listenAddr,
		"ips":                  ips,
		"token":                maskToken(authToken),
		"tls_cert_fingerprint": fingerprint,
	})

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleConn(w, r, logger, authToken, limiter)
	})

	if err := http.Serve(tlsListener, nil); err != nil {
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

func handleConn(w http.ResponseWriter, r *http.Request, logger *Logger, authToken string, limiter *authLimiter) {
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

	if !authenticate(conn, logger, remote, authToken, limiter) {
		return
	}

	pending := map[string]pendingAction{}
	shellUnlocked := false

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
			handleRequest(conn, logger, pending, msg, &shellUnlocked)
		case MsgConfirm:
			handleConfirm(conn, logger, pending, msg, &shellUnlocked)
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
func authenticate(conn *websocket.Conn, logger *Logger, remote, authToken string, limiter *authLimiter) bool {
	if !limiter.allow() {
		logger.Log("auth_failed", "", map[string]string{"remote": remote, "reason": "locked_out"})
		conn.WriteJSON(Message{Type: MsgError, Error: "auth_failed"})
		return false
	}

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
		// Un token vide ne compte pas comme un échec pour le verrou
		// anti-bruteforce : c'est la sonde volontaire utilisée par la
		// découverte automatique (client/discovery.py) pour confirmer
		// qu'il s'agit bien d'un agent DEPAN PC, pas une tentative de
		// devinette — la répéter n'apprend rien à un vrai attaquant.
		if msg.Token != "" {
			limiter.recordFailure()
		}
		logger.Log("auth_failed", "", map[string]string{"remote": remote, "reason": "bad_token"})
		conn.WriteJSON(Message{ID: msg.ID, Type: MsgError, Error: "auth_failed"})
		return false
	}

	limiter.recordSuccess()
	logger.Log("auth_success", "", map[string]string{"remote": remote})
	conn.WriteJSON(Message{ID: msg.ID, Type: MsgResponse, Status: "ok"})
	return true
}

func handleRequest(conn *websocket.Conn, logger *Logger, pending map[string]pendingAction, msg Message, shellUnlocked *bool) {
	def, ok := whitelist[msg.Command]
	if !ok {
		logger.Log("rejected_unknown_command", msg.Command, nil)
		conn.WriteJSON(Message{ID: msg.ID, Type: MsgError, Error: "unknown_command"})
		return
	}

	// run_command reste dans la whitelist en catégorie "read" (pas de
	// confirmation par appel, comme voulu), mais exige d'avoir déverrouillé
	// le mode shell au préalable via enable_shell (une confirmation, une
	// seule fois par connexion) — vérifié ici, avant tout dispatch.
	if msg.Command == "run_command" && !*shellUnlocked {
		logger.Log("rejected_shell_locked", msg.Command, nil)
		conn.WriteJSON(Message{ID: msg.ID, Type: MsgError, Error: "shell_not_unlocked"})
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

func handleConfirm(conn *websocket.Conn, logger *Logger, pending map[string]pendingAction, msg Message, shellUnlocked *bool) {
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

	if action.Command == "enable_shell" {
		*shellUnlocked = true
		logger.Log("shell_unlocked", "", nil)
	}

	logger.Log("action_executed", action.Command, result)
	conn.WriteJSON(Message{ID: msg.ID, Type: MsgResponse, Status: "ok", Result: result})
}

func newToken() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// tokenAlphabet exclut les caractères ambigus à l'oral/à l'écrit (I, L, O,
// 0, 1) : 32 symboles, ce qui tombe rond sur 256/32=8 - aucun biais de
// modulo à corriger.
const tokenAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// newHumanToken génère un token de connexion court (format "XXX-XXX", 6
// caractères utiles, ~30 bits d'entropie) pensé pour être lu à voix haute
// ou retapé rapidement par l'opérateur - contrairement à newToken (64 bits,
// jamais tapé par un humain, réservé aux confirmations internes), la
// commodité prime ici sur l'entropie maximale. Choix assumé par
// l'utilisateur du projet après discussion du compromis.
func newHumanToken() string {
	b := make([]byte, 6)
	rand.Read(b)
	chars := make([]byte, 6)
	for i, v := range b {
		chars[i] = tokenAlphabet[int(v)%len(tokenAlphabet)]
	}
	return fmt.Sprintf("%s-%s", chars[:3], chars[3:])
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
