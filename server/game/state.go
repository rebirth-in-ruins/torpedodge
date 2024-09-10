package game

import (
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"math/rand/v2"

	"github.com/rebirth-in-ruins/torpedodge/server/datastr"
	"github.com/rebirth-in-ruins/torpedodge/server/protocol"
)

// TODO: Maybe just "Game" might be the better name
type State struct {
	sync.Mutex

	// entities on the battlefield
	players map[int]*Player
	airstrikes map[int]*Airstrike
	explosions map[int]*Explosion
	bombs map[int]*Bomb
	corpses map[int]*Corpse
	loot map[int]*Loot

	// input of every player
	inputs map[int]Input

	// give IDs to airstrikes
	counter int

	// entities placed on a coordinate system
	playerPositions [][]*Player
	airstrikePositions [][]*Airstrike
	explosionPositions [][]*Explosion
	bombPositions [][]*Bomb
	corpsePositions [][]*Corpse
	lootPositions [][]*Loot

	// game settings
	Settings Settings

	// recent events that happened (player hit, death)
	Events []string

	// game tells which client to disconnect (because of death)
	Disconnect chan int
}

const (
	// score gained from surviving a turn
	scoreGainTurn = 1

	// score gained from hitting a player
	scoreGainHit = 5

	// an explosion coming from airstrike gets this playerID
	airstrikeID = 0

	// different types of loot grant score
	scoreGainMediocreLoot = 6
	scoreGainGoodLoot = 12

	// TODO: Should be settings
	mediocreLootCount = 3
	goodLootCount = 1
)

func (g *State) RunSimulation() {
	g.Lock()
	defer g.Unlock()

	// Sort inputs by time we received them
	inputs := slices.Collect(maps.Values(g.inputs))
	slices.SortFunc(inputs, func(a Input, b Input) int {
		return a.time.Compare(b.time)
	})

	// Apply inputs:
	// - Move players (and check collision)
	// - Drop bombs the player placed
	for _, input := range inputs {

		// Ignore dead players
		player, found := g.players[input.id]
		if !found || player.IsDead() {
			continue
		}

		// Ignore input if player is charging laser
		if player.Charging {
			continue
		}

		switch payload := input.message.(type) {
		case protocol.Move:
			g.movePlayer(input.id, Direction(payload.Direction))
		case protocol.Bomb:
			g.spawnBomb(input.id)
		}
	}

	// Drop more airstrikes
	g.spawnAirstrike()

	// Remove explosions from previous turn
	clear(g.explosions)
	g.explosionPositions = datastr.NewGrid[Explosion](g.Settings.GridSize)

	// Decay corpses some more
	for _, corpse := range g.corpses {
		corpse.DeathTimer -= 1
		if corpse.IsDead() {
			g.removeCorpse(corpse)
		}
	}

	// Shorten the fuse / detonate airstrikes
	for _, airstrike := range g.airstrikes {
		airstrike.FuseCount -= 1
		if airstrike.Detonated() {
			g.removeAirstrike(airstrike)
		}
	}

	// Shorten the fuse / detonate bombs
	for _, bomb := range g.bombs {
		bomb.FuseCount -= 1
		if bomb.Detonated() {
			g.removeBomb(bomb)
		}
	}

	// Players that charged their laser the previous turn will shoot now
	for _, player := range g.players {
		if player.Charging {
			g.spawnLaser(player)
		}
	}


	// Find players that were hit by explosion
	for _, player := range g.players {
		if explosion := g.explosionPositions[player.X][player.Y]; explosion != nil {
			player.LoseHealth()

			// Grant score to player if hit by them
			if explosion.PlayerID != airstrikeID {
				hitter := g.players[explosion.PlayerID]
				hitter.Score += scoreGainHit

				if hitter.ID == player.ID {
					g.addEvent("%v hurt itself in confusion", player.Name)
				} else {
					g.addEvent("%v got hit by %v", player.Name, hitter.Name)
				}
			} else {
				g.addEvent("%v took a hit", player.Name)
			}
		}
	}

	// Disconnect players that were dead for long enough
	for _, player := range g.players {
		if player.IsDead() {
			g.sinkShip(player)
			g.spawnCorpse(player)
			g.Disconnect <- player.ID
		}
	}

	// Grant everyone a point for surviving
	for _, player := range g.players {
		player.Score += 1
	}

	// Let players charge their laser 
	for _, input := range inputs {
		if _, ok := input.message.(protocol.Laser); ok {
			g.chargeLaser(input.id)
		}
	}

	// Let players get score if they picked up loot
	g.checkLoot()

	// Let new players join after everything is safe
	for _, input := range inputs {
		if payload, ok := input.message.(protocol.Join); ok {
			g.spawnPlayer(input.id, payload.Name, payload.Team)
		}
	}

	// Prepare next round
	clear(g.inputs)
}

// spawnPlayer places the player entity for a client at a random tile
func (g *State) spawnPlayer(id int, name string, team string) {
	x, y := g.getFreeRandomTile()

	player := &Player{
		ID:          id,
		Name:        name,
		X:           x,
		Y:           y,
		Rotation:    Left,
		Charging:    false,
		Team:        team,
		Score:       0,
		Health:      g.Settings.StartHealth,
		BombCount:   g.Settings.InventorySize,
		BombRespawn: 0,
		deathTimer:  g.Settings.DeathTime,
	}

	g.playerPositions[x][y] = player
	g.players[id] = player

	g.addEvent("%v joined", player.Name)
	slog.Info("player joined", slog.String("name", player.Name))
}


// spawnBomb starts a new count down to explosion at a player's position
func (g *State) spawnBomb(id int) {
	player, ok := g.players[id]
	if !ok {
		panic("could not find player") // TODO:
	}

	// Don't allow stacking bombs
	if g.bombPositions[player.X][player.Y] != nil {
		return
	}

	// Player needs to have bombs in his inventory
	if player.BombCount == 0 {
		return
	}

	bomb := &Bomb{
		ID:        g.newID(),
		PlayerID: id,
		X:         player.X,
		Y:         player.Y,
		FuseCount: g.Settings.BombFuseLength,
	}

	g.bombPositions[player.X][player.Y] = bomb
	g.bombs[bomb.ID] = bomb

	// Player has one less bomb in his stock now
	player.BombCount--

	slog.Info("bomb dropped", slog.Int("id", bomb.ID), slog.String("player", player.Name))
}

// spawnAirstrike starts a new count down to explosion at a random tile
func (g *State) spawnAirstrike() {
	x, y := g.getFreeRandomTile()

	airstrike := &Airstrike{
		ID: g.newID(),
		X:         x,
		Y:         y,
		FuseCount: g.Settings.AirstrikeFuseLength,
	}

	g.airstrikePositions[x][y] = airstrike
	g.airstrikes[airstrike.ID] = airstrike

	slog.Debug("airstrike spawned", slog.Int("x", airstrike.X), slog.Int("y", airstrike.Y))
}

// spawnExplosion adds the entity at the location.
// Helps with detecting if players were hit
func (g *State) spawnExplosion(x int, y int, playerID int) {
	// Don't let players spawn explosions outside the grid
	if g.isOutOfBounds(x, y) {
		return
	}

	explosion := &Explosion{
		ID:       g.newID(),
		X:        x,
		Y:        y,
		PlayerID: playerID,
	}

	g.explosionPositions[x][y] = explosion
	g.explosions[explosion.ID] = explosion

	slog.Debug("explosion spawned", slog.Int("x", explosion.X), slog.Int("y", explosion.Y))
}


func (g *State) spawnCorpse(player *Player) {
	corpse := &Corpse{
		ID:         g.newID(),
		Name:       player.Name,
		X:          player.X,
		Y:          player.Y,
		Rotation:   player.Rotation,
		DeathTimer: g.Settings.DeathTime,
	}

	g.corpsePositions[corpse.X][corpse.Y] = corpse
	g.corpses[corpse.ID] = corpse

	g.addEvent("%v died", player.Name)
	slog.Debug("corpse spawned", slog.Int("x", corpse.X), slog.Int("y", corpse.Y))
}

func (g *State) spawnLoot(typ string, value int) {
	x, y := g.getFreeRandomTile()

	loot := &Loot{
		ID:    g.newID(),
		Type:  typ,
		Value: value,
		X:     x,
		Y:     y,
	}

	g.lootPositions[loot.X][loot.Y] = loot
	g.loot[loot.ID] = loot

	slog.Debug("loot spawned", slog.Int("x", loot.X), slog.Int("y", loot.Y))
}

// spawns explosions in front of the player
func (g *State) spawnLaser(player *Player) {
	rotation := player.Rotation
	switch rotation {
	case Down:
		for i := player.Y+1; i < g.Settings.GridSize; i++ {
			g.spawnExplosion(player.X, i, player.ID)
		}
	case Left:
		for i := player.X-1; i >= 0; i-- {
			g.spawnExplosion(i, player.Y, player.ID)
		}
	case Right:
		for i := player.X+1; i < g.Settings.GridSize; i++ {
			g.spawnExplosion(i, player.Y, player.ID)
		}
	case Up:
		for i := player.Y-1; i >= 0; i-- {
			g.spawnExplosion(player.X, i, player.ID)
		}
	}

	player.Charging = false
}

// A websocket client will report that a player has left because their connection is gone.
// TODO: Can this be made private?
func (g *State) RemovePlayer(id int) {
	g.Lock()
	defer g.Unlock()

	player, ok := g.players[id]
	if !ok {
		return // TODO: This means it was already deleted ig
	}
	delete(g.players, id)

	g.playerPositions[player.X][player.Y] = nil
}

// same as above but without locking TODO
func (g *State) sinkShip(player *Player) {
	delete(g.players, player.ID)
	g.playerPositions[player.X][player.Y] = nil
}

// removeAirstrike removes the entity and replaces it 
// with explosions at the location
func (g *State) removeAirstrike(airstrike *Airstrike) {
	delete(g.airstrikes, airstrike.ID)
	g.airstrikePositions[airstrike.X][airstrike.Y] = nil

	// Spawn explosions at the place where the airstrike detonated
	for i := 0; i < g.Settings.GridSize; i++ {
		g.spawnExplosion(airstrike.X, i, airstrikeID);
		g.spawnExplosion(i, airstrike.Y, airstrikeID);
	}
}

// removeBomb removes the entity and replaces it 
// with explosions at the location
func (g *State) removeBomb(bomb *Bomb) {
	delete(g.bombs, bomb.ID)
	g.bombPositions[bomb.X][bomb.Y] = nil

	// Player has a new bomb back in stock
	player, ok := g.players[bomb.PlayerID]
	if !ok {
		panic("player not found") // TODO:
	}
	player.BombCount++

	// Spawn explosions at the place where the bomb detonated
	for i := 0; i < g.Settings.GridSize; i++ {
		g.spawnExplosion(bomb.X, i, bomb.PlayerID);
		g.spawnExplosion(i, bomb.Y, bomb.PlayerID);
	}
}

// removeCorpse removes any remains of the player now
func (g *State) removeCorpse(corpse *Corpse) {
	delete(g.corpses, corpse.ID)
	g.corpsePositions[corpse.X][corpse.Y] = nil
}

// movePlayer changes the position
func (g *State) movePlayer(id int, direction Direction) {
	player, ok := g.players[id]
	if !ok {
		panic("could not get player") // TODO:
	}

	newX := player.X
	newY := player.Y

	switch(direction) {
	case Left:
		newX -= 1
	case Right:
		newX += 1
	case Up:
		newY -= 1
	case Down:
		newY += 1
	}


	// Don't leave map
	if g.isOutOfBounds(newX, newY) {
		return
	}

	// Don't collide with other players
	neighbour := g.playerPositions[newX][newY]
	if neighbour != nil {
		return
	}

	g.playerPositions[player.X][player.Y] = nil
	g.playerPositions[newX][newY] = player
	player.X = newX
	player.Y = newY
	player.Rotation = direction
}

func (g *State) addEvent(format string, args ...any) {
	g.Events = slices.Insert(g.Events, 0, fmt.Sprintf(format, args...))
	g.Events = g.Events[:min(len(g.Events), 8)] // Upper limit on events
}

func (g *State) checkLoot() {
	// Check if player picked up loot
	for _, loot := range g.loot {
		player := g.playerPositions[loot.X][loot.Y]
		if player != nil {
			player.Score += loot.Value
			delete(g.loot, loot.ID)
			g.addEvent("%v found some %v loot", player.Name, loot.Type)
		}
	}

	// one good loot and three bad loot types need to exist at all times
	// TODO: A little much hardcoded lol
	mediocre := 0
	good := 0
	for _, loot := range g.loot {
		if loot.Type == "mediocre" {
			mediocre++
		} else if loot.Type == "good" {
			good++
		}
	}

	// Spawn loot if picked up somewhere
	for i := mediocre; i < mediocreLootCount; i++ {
		g.spawnLoot("mediocre", scoreGainMediocreLoot)
	}
	for i := good; i < goodLootCount; i++ {
		g.spawnLoot("good", scoreGainGoodLoot)
	}
}

func (g *State) chargeLaser(id int) {
	player, ok := g.players[id]
	if !ok {
		panic("could not get player") // TODO:
	}

	player.Charging = true
}

// newID hands out a new unique ID for spawning new entities
func (g *State) newID() int {
	result := g.counter
	g.counter++
	return result
}

// isOutOfBounds keeps players from moving out of the map.
func (g *State) isOutOfBounds(x int, y int) bool {
	horizontal := x < 0 || g.Settings.GridSize <= x
	vertical := y < 0 || g.Settings.GridSize <= y

	return horizontal || vertical;
}

// getFreeRandomTile helps in finding a spawn location for entites.
func (g *State) getFreeRandomTile() (int, int) {
	var x, y int
	for {
		x = rand.IntN(g.Settings.GridSize)
		y = rand.IntN(g.Settings.GridSize)

		// Retry TODO: Inefficient
		if g.playerPositions[x][y] != nil || g.airstrikePositions[x][y] != nil || g.bombPositions[x][y] != nil || g.lootPositions[x][y] != nil {
			continue
		}

		return x, y
	}
}

type Input struct {
	id int
	message protocol.Message
	time time.Time
}

func (i Input) String() string {
	return fmt.Sprintf("%v:{%s}", i.id ,i.message)
}

// TODO: Some messages should be evaluated immediately and the state should be sent to spectators (like join, direction known)
// Needs to be thread-safe.
func (g *State) StoreInput(id int, message protocol.Message) {
	g.Lock()
	defer g.Unlock()

	g.inputs[id] = Input{id: id, message: message, time: time.Now()}
}

// New starts a fresh game state.
func New(settings Settings) *State {
	return &State{
		Mutex:              sync.Mutex{},
		players:            make(map[int]*Player),
		airstrikes:         make(map[int]*Airstrike),
		explosions:         make(map[int]*Explosion),
		bombs:              make(map[int]*Bomb),
		corpses:            make(map[int]*Corpse),
		loot:               make(map[int]*Loot),
		inputs:             make(map[int]Input),
		counter:            0,
		playerPositions:    datastr.NewGrid[Player](settings.GridSize),
		airstrikePositions: datastr.NewGrid[Airstrike](settings.GridSize),
		explosionPositions: datastr.NewGrid[Explosion](settings.GridSize),
		bombPositions:      datastr.NewGrid[Bomb](settings.GridSize),
		corpsePositions:    datastr.NewGrid[Corpse](settings.GridSize),
		lootPositions:      datastr.NewGrid[Loot](settings.GridSize),
		Settings:           settings,
		Events:             make([]string, 0),
		Disconnect:         make(chan int, 10),
	}
}

