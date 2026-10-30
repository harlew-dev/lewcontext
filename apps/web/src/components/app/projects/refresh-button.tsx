import { ArrowPathIcon } from "@heroicons/react/16/solid";
import { Button } from "@/components/ui/button";
import type { ProjectsState } from "./use-projects";
export function RefreshButton({ state }: { state: ProjectsState }) {
  return (
    <Button
      plain
      aria-label="Refresh projects"
      loading={state.refreshing}
      onClick={state.refresh}
    >
      <ArrowPathIcon />
    </Button>
  );
}
