# Guide utilisateur Tanpopo

**Tanpopo** est une passerelle IA et un Hub MCP (Model Context Protocol) auto-hébergé en binaire unique, conçu pour le routage déterministe, la sécurité des secrets en écriture seule, la comptabilité FinOps exacte en micro-USD et le fonctionnement hors ligne.

---

## 1. Démarrage rapide & configuration initiale

1. Démarrez le serveur Tanpopo :
   ```bash
   tanpopo serve --role all --listen 127.0.0.1:8080
   ```
2. Au premier lancement, Tanpopo affiche un **jeton d'amorçage** à usage unique dans les journaux serveur.
3. Ouvrez `http://127.0.0.1:8080/setup` dans votre navigateur et suivez l'assistant en 4 étapes :
   - **Compte propriétaire :** Saisissez le jeton d'amorçage, l'e-mail administrateur, le mot de passe (12 caractères minimum), le nom de l'organisation et du premier projet.
   - **Première connexion (facultatif) :** Connectez une API commerciale (OpenAI, Anthropic, Google Gemini ou compatible OpenAI) ou un moteur local (vLLM, Ollama). Les cibles locales imposent d'autoriser explicitement le réseau privé et de lister des CIDR précis sans joker (ex. `127.0.0.1/32`).
   - **Garde de la clé :** Confirmez la sauvegarde de votre clé maître de chiffrement (Master KEK). Sa perte rend les secrets chiffrés irrécupérables.
   - **Vérification & initialisation :** Terminez la configuration, conservez votre clé virtuelle affichée une seule fois et vérifiez la première requête en streaming.

---

## 2. Langue & apparence

- **Console Web :** Utilisez le sélecteur de langue dans la barre supérieure (ainsi que sur les pages Configuration et Connexion). **Auto** est sélectionné par défaut et choisit la première langue prise en charge par votre navigateur (repli sur English). Un choix manuel persiste au rechargement et vous pouvez revenir à **Auto** à tout moment.
- **CLI :** Utilisez `--lang <code>` (ex. `tanpopo --lang fr --help`) ou les variables `TANPOPO_LANG`, `LC_ALL`, `LC_MESSAGES`, `LANG`. Les noms de commandes, options et sorties machine restent stables.
- **Erreurs API & alertes :** Les routes `/admin/v1/*` utilisent l'en-tête `Accept-Language` pour localiser `error.message` et les résumés d'alertes tout en gardant `error.code`, `error.type` et les schémas JSON inchangés.
- **Fonctionnement hors ligne :** Les 15 langues, polices et icônes sont embarquées dans le binaire — zéro requête CDN externe à l'exécution.

---

## 3. Flux principaux de la console

### Vue d'ensemble & Traces
- **Vue d'ensemble** affiche les requêtes en direct, le taux d'erreur, le surcoût proxy p95, les dépenses réglées en micro-USD, les retenues actives, les déploiements à circuit ouvert et les alertes actives avec liens de remédiation.
- **Traces** enregistre une télémétrie uniquement basée sur les métadonnées (aucun prompt ni réponse). Cliquez sur une requête pour inspecter sa cascade d'étapes et sa précision (`Exact`, `Estimé` ou `Non mesuré`).

### Fournisseurs, Modèles & Routage
- **Fournisseurs :** Scellez clés API, identifiants AWS IAM, secrets HMAC ou certificats TLS dans le coffre en écriture seule. Associez les adaptateurs et testez les connexions en direct.
- **Modèles & déploiements :** Déclarez les modèles logiques et associez-les à des grilles tarifaires entières exactes en **nano-USD**.
- **Routes & politiques :** Configurez le routage déterministe (`Bascule par priorité`, `Pondéré`, `Tourniquet pondéré`, `Coût minimal`, `Latence minimale (EWMA)`). Utilisez **Simuler une route** à blanc sans trafic sortant. Les politiques hiérarchiques ne peuvent que renforcer les bornes parentes.

### Extensions, Hub MCP & Bac à sable
- **Extensions :** Préparez des paquets depuis le catalogue hors ligne ou via une URI + SHA-256 à 64 caractères. Examinez les différences de permissions avant activation et mettez immédiatement en quarantaine tout paquet compromis.
- **Hub MCP :** Enregistrez des serveurs Streamable HTTP, synchronisez les outils, épinglez les hashs de schéma RFC 8785 JCS, classez les risques (`read_only`, `mutating`, `destructive`) et accordez les accès par clé virtuelle.
- **Bac à sable :** Testez des prompts avec une clé éphémère plafonnée en budget et copiez des extraits prêts à l'emploi en cURL, Python et TypeScript.

### Accès, FinOps, Sécurité & Exploitation
- **Accès & clés :** Émettez des clés virtuelles par environnement, faites-les pivoter avec 24 h de grâce ou révoquez-les en vidage progressif ou arrêt d'urgence. Gérez les 6 rôles RBAC.
- **FinOps :** Suivez dépenses exactes, estimées, retenues actives et requêtes non mesurées ; définissez les plafonds en micro-USD et exportez en CSV/JSON.
- **Sécurité :** Validez les approbations humaines (HITL) pour les appels MCP destructifs, gérez les arrêts d'urgence à 4 niveaux et vérifiez la chaîne d'audit cryptographique RFC 8785 JCS.
- **Exploitation (rôle `OPERATOR`) :** Surveillez `/readyz` et les époques du cluster, créez des sauvegardes chiffrées `.tpop.enc`, lancez les pré-vérifications de mise à niveau et enregistrez des webhooks signés HMAC.

---

## 4. Aide-mémoire CLI

| Commande | Usage |
| --- | --- |
| `tanpopo serve` | Démarrer le serveur Tanpopo (`--role all`, `control-plane` ou `data-plane`). |
| `tanpopo doctor` | Vérifier les fichiers locaux, la base de données et la santé du service (`--output json`). |
| `tanpopo support-bundle` | Créer une archive de diagnostic expurgée (sans secrets ni prompts). |
| `tanpopo admin reset-local-owner` | Réinitialiser hors ligne le mot de passe du propriétaire local. |
| `tanpopo backup create / verify / restore` | Créer, vérifier et restaurer des sauvegardes chiffrées `.tpop.enc`. |
| `tanpopo plugin init / validate / test / pack` | Créer, tester hors ligne et empaqueter des extensions `.tpkg`. |
| `tanpopo upgrade preflight` | Valider la compatibilité ABI avant une mise à niveau de Tanpopo Core. |
| `tanpopo key / route / budget / audit / killswitch` | Consulter les ressources distantes du plan de contrôle via `/admin/v1`. |
