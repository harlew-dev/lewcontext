import { ArrowPathIcon } from "@heroicons/react/16/solid";
import { Button } from "@/components/ui/button";
export function RefreshButton({
  refreshing,
  onRefresh,
}: {
  refreshing: boolean;
  onRefresh: () => void;
}) {
  return (
    <Button
      plain
      aria-label="Refresh projects"
      loading={refreshing}
      onClick={onRefresh}
    >
      <ArrowPathIcon />
    </Button>
  );
}
