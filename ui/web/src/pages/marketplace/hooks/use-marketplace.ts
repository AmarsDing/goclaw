import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { toast } from "@/stores/use-toast-store";
import { queryKeys } from "@/lib/query-keys";

export interface MarketplacePackage {
  id: string;
  name: string;
  type: string;
  description: string;
  author: string;
  version: string;
  ref?: string;
  readme_markdown?: string;
  likes?: number;
  downloads?: number;
  rating?: number;
  rating_count?: number;
  review_state?: string;
  pricing?: {
    model?: string;
    price?: number;
    currency?: string;
    trial_days?: number;
  };
}

export interface MarketplaceReview {
  id: string;
  package_id: string;
  user_id: string;
  rating: number;
  comment: string;
  created_at: string;
}

export interface InstalledMarketplacePackage {
  package_id: string;
  name: string;
  version: string;
  ref?: string;
  type: string;
  installed_at: string;
  path: string;
}

interface MarketplaceSearchResponse {
  packages: MarketplacePackage[];
  total: number;
  limit: number;
  offset: number;
}

interface MarketplaceInstalledResponse {
  packages: InstalledMarketplacePackage[];
}

interface MarketplaceReviewResponse {
  reviews: MarketplaceReview[];
}

function useMarketplaceActions(search: string) {
  const http = useHttp();
  const qc = useQueryClient();

  const installPackage = useCallback(async (pkg: MarketplacePackage) => {
    try {
      const res = await http.post<InstalledMarketplacePackage>(`/v1/marketplace/packages/${pkg.id}/install`, {});
      toast.success(`Installed ${pkg.name}`);
      qc.invalidateQueries({ queryKey: queryKeys.marketplace.installed });
      qc.invalidateQueries({ queryKey: queryKeys.marketplace.list({ search }) });
      qc.invalidateQueries({ queryKey: queryKeys.marketplace.detail(pkg.id) });
      return { ok: true, result: res };
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      toast.error(`Failed to install ${pkg.name}: ${message}`);
      return { ok: false, error: message };
    }
  }, [http, qc, search]);

  const startTrial = useCallback(
    async (pkg: MarketplacePackage) => {
      try {
        await http.post(`/v1/marketplace/packages/${pkg.id}/trial`, {});
        toast.success(`Trial started for ${pkg.name}`);
        qc.invalidateQueries({ queryKey: queryKeys.marketplace.detail(pkg.id) });
        return { ok: true as const };
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        toast.error(message);
        return { ok: false as const, error: message };
      }
    },
    [http, qc],
  );

  const purchaseDev = useCallback(
    async (pkg: MarketplacePackage) => {
      try {
        await http.post(`/v1/marketplace/packages/${pkg.id}/purchase`, {});
        toast.success(`Access granted for ${pkg.name}`);
        qc.invalidateQueries({ queryKey: queryKeys.marketplace.detail(pkg.id) });
        return { ok: true as const };
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        toast.error(message);
        return { ok: false as const, error: message };
      }
    },
    [http, qc],
  );

  const likePackage = useCallback(async (pkg: MarketplacePackage) => {
    try {
      await http.post<MarketplacePackage>(`/v1/marketplace/packages/${pkg.id}/like`, {});
      toast.success(`Liked ${pkg.name}`);
      qc.invalidateQueries({ queryKey: queryKeys.marketplace.list({ search }) });
      qc.invalidateQueries({ queryKey: queryKeys.marketplace.detail(pkg.id) });
      return { ok: true };
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      toast.error(`Failed to like ${pkg.name}: ${message}`);
      return { ok: false, error: message };
    }
  }, [http, qc, search]);

  const loadReviews = useCallback(async (packageID: string) => {
    return http.get<MarketplaceReviewResponse>(`/v1/marketplace/packages/${packageID}/reviews`);
  }, [http]);

  const addReview = useCallback(async (pkg: MarketplacePackage, rating: number, comment: string) => {
    try {
      await http.post<MarketplacePackage>(`/v1/marketplace/packages/${pkg.id}/review`, { rating, comment });
      toast.success(`Review submitted for ${pkg.name}`);
      qc.invalidateQueries({ queryKey: queryKeys.marketplace.list({ search }) });
      qc.invalidateQueries({ queryKey: queryKeys.marketplace.detail(pkg.id) });
      return { ok: true };
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      toast.error(`Failed to review ${pkg.name}: ${message}`);
      return { ok: false, error: message };
    }
  }, [http, qc, search]);

  return {
    installPackage,
    likePackage,
    loadReviews,
    addReview,
    startTrial,
    purchaseDev,
  };
}

export function useMarketplace(search: string) {
  const http = useHttp();
  const connected = useAuthStore((s) => s.connected);
  const actions = useMarketplaceActions(search);

  const { data, isFetching: loading, refetch } = useQuery({
    queryKey: queryKeys.marketplace.list({ search }),
    queryFn: () =>
      http.get<MarketplaceSearchResponse>(
        `/v1/marketplace/packages?query=${encodeURIComponent(search)}&limit=50&sort_by=updated`,
      ),
    staleTime: 30_000,
    enabled: connected,
  });

  const { data: installedData, isFetching: installedLoading, refetch: refetchInstalled } = useQuery({
    queryKey: queryKeys.marketplace.installed,
    queryFn: () => http.get<MarketplaceInstalledResponse>("/v1/marketplace/installed"),
    staleTime: 30_000,
    enabled: connected,
  });

  const refresh = useCallback(() => {
    refetch();
    refetchInstalled();
  }, [refetch, refetchInstalled]);

  const installedIDs = new Set((installedData?.packages ?? []).map((pkg) => pkg.package_id));

  return {
    packages: data?.packages ?? [],
    total: data?.total ?? 0,
    installedPackages: installedData?.packages ?? [],
    installedIDs,
    loading,
    installedLoading,
    refresh,
    ...actions,
  };
}

export function useMarketplacePackage(packageID?: string) {
  const http = useHttp();
  const connected = useAuthStore((s) => s.connected);
  const actions = useMarketplaceActions("");

  const { data, isFetching: loading, refetch } = useQuery({
    queryKey: queryKeys.marketplace.detail(packageID ?? ""),
    queryFn: () => http.get<MarketplacePackage>(`/v1/marketplace/packages/${packageID}`),
    staleTime: 30_000,
    enabled: connected && !!packageID,
  });

  const { data: installedData, isFetching: installedLoading, refetch: refetchInstalled } = useQuery({
    queryKey: queryKeys.marketplace.installed,
    queryFn: () => http.get<MarketplaceInstalledResponse>("/v1/marketplace/installed"),
    staleTime: 30_000,
    enabled: connected,
  });

  const refresh = useCallback(() => {
    refetch();
    refetchInstalled();
  }, [refetch, refetchInstalled]);

  const installedIDs = new Set((installedData?.packages ?? []).map((pkg) => pkg.package_id));

  return {
    pkg: data ?? null,
    installed: packageID ? installedIDs.has(packageID) : false,
    loading,
    installedLoading,
    refresh,
    ...actions,
  };
}
