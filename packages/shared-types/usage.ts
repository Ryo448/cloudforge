import type { Execution, UsageSample } from "./index";

type ProjectWithUsage = {
  id: string;
  execution: Pick<Execution, "actualState" | "startedAt" | "cpu" | "memory">;
  usageHistory?: UsageSample[];
};

const windowMs = 120_000;

export function mergeProjectUsage<T extends ProjectWithUsage>(
  previous: T[] | null | undefined,
  incoming: T[],
): T[] {
  const oldProjects = new Map(
    previous?.map((project) => [project.id, project]),
  );
  const now = Date.now();
  return incoming.map((project) => {
    const old = oldProjects.get(project.id);
    const previousHistory =
      old?.execution.startedAt === project.execution.startedAt
        ? (old?.usageHistory?.filter(
            (sample) => now - sample.time <= windowMs,
          ) ?? [])
        : [];
    const samples = new Map<number, UsageSample>();
    for (const sample of [
      ...previousHistory,
      ...(project.usageHistory ?? []),
    ]) {
      if (now - sample.time <= windowMs) samples.set(sample.time, sample);
    }
    const history = [...samples.values()].sort((a, b) => a.time - b.time);
    if (
      project.execution.actualState === "online" &&
      (project.execution.cpu != null || project.execution.memory != null) &&
      (!history.length || now - history[history.length - 1].time > 1500)
    ) {
      history.push({
        time: now,
        cpu: project.execution.cpu,
        memory: project.execution.memory,
      });
    }
    return { ...project, usageHistory: history };
  });
}
