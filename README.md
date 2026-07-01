# GitHub → GitLab — Mirroir automatique

Ce dépôt synchronise chaque jour, automatiquement, tous vos dépôts GitHub
(personnels **et** ceux de vos organisations, publics **et** privés) vers un
groupe GitLab. Seuls les dépôts ayant réellement changé depuis la dernière
exécution sont re-poussés. Le projet est écrit en **Go** (plus de script shell).

## Fonctionnement

`.github/workflows/mirror-to-gitlab.yml` déclenche le binaire Go (`go run .`)
tous les jours à 03h00 UTC (et à la demande depuis l'onglet **Actions**). Le
binaire :

1. Liste tous les dépôts accessibles au token (propriétaire + membre
   d'organisation), publics et privés, via l'API GitHub.
2. Pour chacun, compare rapidement les références Git (`git ls-remote`) entre
   GitHub et GitLab — si elles sont identiques, le dépôt est ignoré, sans
   clone ni transfert.
3. Sinon : crée le projet GitLab s'il n'existe pas encore (avec la même
   visibilité que sur GitHub), puis effectue un `git clone --mirror` suivi
   d'un `git push --mirror` (toutes les branches, tags et tout l'historique),
   et aligne la branche par défaut.
4. À la fin, affiche un résumé (synchronisés / ignorés / échecs) et sort avec un code d'erreur si un dépôt a échoué, pour que ce soit visible dans Actions.

⚠️ **Le dépôt GitLab devient une copie exacte de GitHub.** Ne modifiez jamais
directement le code côté GitLab : tout changement y serait écrasé (ou
supprimé) à la prochaine synchronisation.

## Mise en place

### 1. Créer ce dépôt
Créez un nouveau dépôt GitHub (privé de préférence, puisqu'il contiendra la
configuration de cette automatisation) et ajoutez-y les fichiers de ce
projet.

### 2. Créer un groupe GitLab de destination
Créez un **groupe** GitLab vide (ex. `github-mirror`) qui accueillera tous
les projets miroirs. Le script a besoin d'un groupe existant, pas d'un
namespace personnel.

### 3. Générer les jetons d'accès

**Token GitHub** — Settings → Developer settings → Personal access tokens →
Tokens (classic), avec les scopes :
- `repo` (accès complet aux dépôts, y compris privés)
- `read:org` (lister les dépôts des organisations)

**Token GitLab** — Preferences → Access Tokens, avec le scope :
- `api`

### 4. Configurer secrets et variables

Dans **Settings → Secrets and variables → Actions** du nouveau dépôt GitHub :

**Secrets** (onglet *Secrets*) :
| Nom | Valeur |
|---|---|
| `GH_PAT` | le token GitHub de l'étape 3 |
| `GITLAB_TOKEN` | le token GitLab de l'étape 3 |

**Variables** (onglet *Variables*) :
| Nom | Valeur |
|---|---|
| `GITLAB_GROUP` | chemin complet du groupe GitLab cible (ex. `mon-groupe` ou `mon-groupe/sous-groupe`) |
| `GITLAB_HOST` | optionnel — hôte GitLab si instance auto-hébergée (par défaut `gitlab.com`) |

### 5. Premier lancement

Onglet **Actions** → workflow *Mirror GitHub → GitLab* → **Run workflow**,
pour tester avant d'attendre le déclenchement automatique du lendemain.

## ⚠️ Point d'attention : désactivation par inactivité

GitHub désactive automatiquement les workflows planifiés (`schedule`) d'un
dépôt public resté sans activité pendant 60 jours (et le même comportement
est régulièrement rapporté sur des dépôts privés). Comme ce dépôt ne reçoit
jamais de commit par lui-même, le workflow risque de s'arrêter silencieusement
au bout de deux mois. Deux options :

- Repassez de temps en temps sur l'onglet **Actions** pour vérifier qu'il est
  toujours *Enabled*, et cliquez sur **Enable workflow** si besoin.
- Ou ajoutez un commit occasionnel (par exemple sur ce README), ce qui
  réinitialise le compteur d'inactivité.

## Aller plus loin

- **Parallélisme** : le nombre de workers (goroutines qui synchronisent en parallèle) est configurable via la variable `WORKERS` (défaut : 5).
- **Timeout** : configurable dans `main.go` (par défaut 30 minutes).
- **Notifications** : une étape `if: failure()` peut poster sur Slack/e-mail en cas d'échec.
- **Exclure les forks** : ajouter un filtre `!r.GetFork()` dans `internal/github/client.go`.

N'hésitez pas à demander si vous voulez une de ces variantes.
