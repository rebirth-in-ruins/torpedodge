import Player from './player';
import Bomb from './bomb';
import Explosion from './explosion';
import Airstrike from './airstrike';
import { ServerAirstrike, ServerBomb, ServerExplosion, ServerPlayer } from './main';

export default class Battlefield
{
    private GRID_PERCENTAGE = 0.85;
    private gridCount: number;

    private _tileSize: number;

    private MARGIN_LEFT: number = 10;
    private MARGIN_TOP: number = 10;

    private scene: Phaser.Scene;

    private _players: Map<number, Player> = new Map(); 
    private _airstrikes: Map<number, Airstrike> = new Map(); 
    private _bombs: Map<number, Bomb> = new Map(); 

    constructor(scene: Phaser.Scene, gameWidth: number, gameHeight: number, gridCount: number)
    {
        this.gridCount = gridCount;

        const gridSize = gameHeight*this.GRID_PERCENTAGE;
        this._tileSize = (gameHeight*this.GRID_PERCENTAGE)/this.gridCount;

        // Draw black background offset by a 2px background (serves as the grid border).
        const r1 = scene.add.rectangle(this.MARGIN_LEFT-2, this.MARGIN_TOP-2, gridSize+2, gridSize+2, 0x000000, 0.5);
        r1.setOrigin(0,0);

        // Draw the grid on top
        const g1 = scene.add.grid(this.MARGIN_LEFT, this.MARGIN_TOP, gridSize, gridSize, this._tileSize, this._tileSize, 0x0000cc, 1, 0x000000, 0.5);
        g1.setOrigin(0,0);

        this.scene = scene;
    }


    renderPlayer(obj: ServerPlayer)
    {
        const player = new Player(this.scene, obj.name, obj.health, obj.bombCount, this._tileSize);
        this._players.set(obj.id, player);

        const [worldX, worldY] = this.gridToWorld(obj.x, obj.y);
        player.x = worldX;
        player.y = worldY;
        player.lookDirection(obj.rotation);
    }

    renderAirstrike(obj: ServerAirstrike)
    {
        const airstrike = new Airstrike(this.scene, obj.fuseCount);
        this._airstrikes.set(obj.id, airstrike);

        const [worldX, worldY] = this.gridToWorld(obj.x, obj.y);
        airstrike.x = worldX;
        airstrike.y = worldY;
    }

    renderBomb(obj: ServerBomb)
    {
        const bomb = new Bomb(this.scene, obj.fuseCount);
        this._bombs.set(obj.id, bomb);

        const [worldX, worldY] = this.gridToWorld(obj.x, obj.y);
        bomb.x = worldX;
        bomb.y = worldY;
    }

    renderExplosions(obj: ServerExplosion, sound: Phaser.Sound.NoAudioSound | Phaser.Sound.HTML5AudioSound | Phaser.Sound.WebAudioSound, focusLost: boolean)
    {
        if(focusLost)
            return

        const explosion = new Explosion(this.scene);

        const [worldX, worldY] = this.gridToWorld(obj.x, obj.y);
        explosion.x = worldX;
        explosion.y = worldY;

        this.scene.cameras.main.shake(200, 0.01);
        sound.play(); // TODO: Varying explosion sounds please
    }

    clearPlayers()
    {
        for(const [id, player] of this._players)
        {
            player.destroy();
            this._players.delete(id);
        }
    }

    clearAirstrikes()
    {
        for(const [id, airstrike] of this._airstrikes)
        {
            airstrike.destroy();
            this._airstrikes.delete(id);
        }
    }

    clearBombs()
    {
        for(const [id, bomb] of this._bombs)
        {
            bomb.destroy();
            this._bombs.delete(id);
        }
    }

    removePlayer(id: number)
    {
        const player = this._players.get(id);
        player.destroy();
        this._players.delete(id);
    }

    gridToWorld(x: number, y: number)
    {
        const worldX = x * this._tileSize + this._tileSize*0.5 + this.MARGIN_LEFT;
        const worldY = y * this._tileSize + this._tileSize*0.5 + this.MARGIN_TOP;

        return [worldX, worldY];
    }
}
