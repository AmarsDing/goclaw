import type { TeamData } from "@/types/team";

/** True when settings.marketplace.published is set. */
export function readTeamMarketplacePublished(team: TeamData): boolean {
  const s = team.settings;
  if (!s || typeof s !== "object") return false;
  const m = (s as Record<string, unknown>).marketplace;
  if (!m || typeof m !== "object") return false;
  return Boolean((m as Record<string, unknown>).published);
}
