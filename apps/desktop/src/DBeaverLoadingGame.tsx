import { ArrowDown, ArrowLeft, ArrowRight, ArrowUp, Play } from "lucide-react";
import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { Spinner } from "../../../packages/ui";
import { t } from "../../../packages/ui/i18n";

const columns = 16;
const rows = 10;
const tickMs = 145;
const directions = [
  {
    key: "ArrowLeft",
    icon: ArrowLeft,
    label: "Mover para a esquerda",
    x: -1,
    y: 0,
  },
  { key: "ArrowUp", icon: ArrowUp, label: "Mover para cima", x: 0, y: -1 },
  { key: "ArrowDown", icon: ArrowDown, label: "Mover para baixo", x: 0, y: 1 },
  {
    key: "ArrowRight",
    icon: ArrowRight,
    label: "Mover para a direita",
    x: 1,
    y: 0,
  },
] as const;

type Direction = (typeof directions)[number];
type Game = {
  snake: number[];
  food: number | null;
  direction: Direction;
  queuedDirection: Direction;
  phase: "ready" | "playing" | "lost" | "won";
};

const initialGame: Game = {
  snake: [5 * columns + 4, 5 * columns + 3, 5 * columns + 2],
  food: 5 * columns + 8,
  direction: directions[3],
  queuedDirection: directions[3],
  phase: "ready",
};

function advance(game: Game, random: number): Game {
  if (game.phase !== "playing") return game;
  const direction = game.queuedDirection;
  const x = (game.snake[0] % columns) + direction.x;
  const y = Math.floor(game.snake[0] / columns) + direction.y;
  const head = y * columns + x;
  const eating = head === game.food;
  // The tail moves out of the way unless this move grows the snake.
  const body = eating ? game.snake : game.snake.slice(0, -1);
  if (x < 0 || x >= columns || y < 0 || y >= rows || body.includes(head)) {
    return { ...game, phase: "lost" };
  }

  const snake = [head, ...body];
  if (!eating) return { ...game, snake, direction };

  const occupied = new Set(snake);
  const free = Array.from({ length: columns * rows }, (_, cell) => cell).filter(
    (cell) => !occupied.has(cell),
  );
  if (!free.length)
    return { ...game, snake, direction, food: null, phase: "won" };
  return {
    ...game,
    snake,
    direction,
    food: free[Math.floor(random * free.length)],
  };
}

function position(cell: number) {
  return {
    gridColumn: (cell % columns) + 1,
    gridRow: Math.floor(cell / columns) + 1,
  };
}

export function DBeaverLoadingGame() {
  const [game, setGame] = useState(initialGame);
  const controls = useRef<HTMLDivElement>(null);
  const playing = game.phase === "playing";

  useEffect(() => {
    if (!playing) return;
    let timer: number | undefined;
    const updateTimer = () => {
      window.clearInterval(timer);
      if (!document.hidden) {
        timer = window.setInterval(() => {
          const random = Math.random();
          setGame((current) => advance(current, random));
        }, tickMs);
      }
    };
    updateTimer();
    document.addEventListener("visibilitychange", updateTimer);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", updateTimer);
    };
  }, [playing]);

  function play() {
    setGame({ ...initialGame, phase: "playing" });
    controls.current?.focus({ preventScroll: true });
  }

  function turn(direction: Direction) {
    setGame((current) => {
      if (
        current.phase !== "playing" ||
        current.direction !== current.queuedDirection
      ) {
        return current;
      }
      if (
        direction.x === -current.direction.x &&
        direction.y === -current.direction.y
      ) {
        return current;
      }
      return { ...current, queuedDirection: direction };
    });
  }

  function handleKey(event: KeyboardEvent<HTMLDivElement>) {
    if (!playing) {
      if (event.target === event.currentTarget && event.key === "Enter") {
        event.preventDefault();
        play();
      }
      return;
    }
    const direction = directions.find(({ key }) => key === event.key);
    if (!direction || event.altKey || event.ctrlKey || event.metaKey) return;
    event.preventDefault();
    turn(direction);
  }

  return (
    <div className="dbeaver-loading">
      <div className="dbeaver-loading-status" role="status">
        <Spinner /> {t("Conectando ao banco…")}
      </div>
      <div
        className="dbeaver-snake"
        role="group"
        aria-labelledby="dbeaver-snake-title"
        aria-describedby="dbeaver-snake-help"
        tabIndex={-1}
        ref={controls}
        onKeyDown={handleKey}
      >
        <div className="dbeaver-snake-heading">
          <h2 id="dbeaver-snake-title">{t("Cobrinha")}</h2>
          <span className="dbeaver-snake-score">
            {t("Pontos")}{" "}
            <strong>{game.snake.length - initialGame.snake.length}</strong>
          </span>
        </div>
        <div className="dbeaver-snake-board">
          <div
            className="dbeaver-snake-grid"
            aria-hidden="true"
            style={{
              aspectRatio: `${columns} / ${rows}`,
              gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))`,
              gridTemplateRows: `repeat(${rows}, minmax(0, 1fr))`,
              backgroundSize: `${100 / columns}% ${100 / rows}%`,
            }}
          >
            {game.snake.map((cell, index) => (
              <span
                key={cell}
                className="dbeaver-snake-segment"
                data-facing={index === 0 ? game.direction.key : undefined}
                style={position(cell)}
              />
            ))}
            {game.food !== null && (
              <span
                className="dbeaver-snake-food"
                style={position(game.food)}
              />
            )}
          </div>
          {!playing && (
            <div className="dbeaver-snake-overlay">
              {game.phase !== "ready" && (
                <p>
                  {game.phase === "won" ? t("Você venceu!") : t("Fim de jogo")}
                </p>
              )}
              <button type="button" className="button primary" onClick={play}>
                <Play size={15} aria-hidden="true" />
                {game.phase === "ready" ? t("Jogar") : t("Jogar novamente")}
              </button>
            </div>
          )}
        </div>
        <div className="dbeaver-snake-controls">
          <span className="muted" id="dbeaver-snake-help">
            {t("Use as setas para jogar")}
          </span>
          <div className="dbeaver-snake-arrows">
            {directions.map((direction) => (
              <button
                key={direction.key}
                type="button"
                className="button"
                aria-label={t(direction.label)}
                disabled={!playing}
                onClick={() => turn(direction)}
              >
                <direction.icon size={16} aria-hidden="true" />
              </button>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
