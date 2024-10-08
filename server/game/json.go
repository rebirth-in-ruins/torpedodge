package game

import (
	"encoding/json"
	"log/slog"

	"github.com/rebirth-in-ruins/torpedodge/server/bestlist"
)

// GameStateResponse lists all entities and their position
// on the battlefield.
type GameStateResponse struct {
	// a list of players on the field
	Players []Player `json:"players"`

	// a list of inbound airstrikes falling from the sky
	Airstrikes []Airstrike `json:"airstrikes"`

	// a list of coordinates where an explosion occured and ships get hit
	Explosions []Explosion `json:"explosions"`

	// a list of bombs dropped by players
	Bombs []Bomb `json:"bombs"`

	// a list of players that died and whose remains remain for some time
	Corpses []Corpse `json:"corpses"`

	// a list of things that can be picked up for points
	Loot []Loot `json:"loot"`

	// the scores of each player
	Leaderboard []Score `json:"leaderboard"`

	// a list of the highest scorers
	Kings []bestlist.Entry `json:"kings"`

	// a list of things that can be picked up for points
	Animations []Animation `json:"animations"`

	// text of recent events
	Events []string `json:"events"`

	// static game settings (mostly relevant for the browser client)
	Settings Settings `json:"settings"`
}

// Score is a tuple of a player's name and their current score
type Score struct {
	Name string `json:"name"`
	Score int `json:"score"`
}

func (g *Game) JSON(lock bool) []byte {
	if lock { // Short-term hack because one caller already has this lock...
		g.Lock()
		defer g.Unlock()
	}

	// Players
	players := make([]Player, 0)
	for _, player := range g.players {
		players = append(players, *player)
	}

	// Airstrikes
	airstrikes := make([]Airstrike, 0)
	for _, airstrike := range g.airstrikes {
		airstrikes = append(airstrikes, *airstrike)
	}

	// Explosions
	explosions := make([]Explosion, 0)
	for _, explosion := range g.explosions {
		explosions = append(explosions, *explosion)
	}

	// Bombs
	bombs := make([]Bomb, 0)
	for _, bomb := range g.bombs {
		bombs = append(bombs, *bomb)
	}

	// Corpses
	corpses := make([]Corpse, 0)
	for _, corpse := range g.corpses {
		corpses = append(corpses, *corpse)
	}

	// Loot
	loot := make([]Loot, 0)
	for _, item := range g.loot {
		loot = append(loot, *item)
	}

	// Leaderboard
	leaderboard := make([]Score, 0)
	for _, player := range g.players {
		leaderboard = append(leaderboard, Score{Name: player.Name, Score: player.Score})
	}

	response := GameStateResponse{
		Players:     players,
		Airstrikes:  airstrikes,
		Explosions:  explosions,
		Bombs:       bombs,
		Corpses:     corpses,
		Loot:        loot,
		Leaderboard: leaderboard,
		Kings:       g.bestlist.GetTop3(),
		Animations:  g.animations,
		Events:      g.events,
		Settings:    g.Settings,
	}

	b, err := json.Marshal(response)
	if err != nil {
		slog.Info("failed marshalling", slog.String("error", err.Error()))
		return []byte{}
	}

	return b
}
