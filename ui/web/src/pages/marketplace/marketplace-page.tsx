import { useMemo, useState } from "react";
import { Link } from "react-router";
import { RefreshCw, Download, Star, Heart, PackageCheck, MessageSquarePlus, ArrowRight } from "lucide-react";
import { PageHeader } from "@/components/shared/page-header";
import { SearchInput } from "@/components/shared/search-input";
import { EmptyState } from "@/components/shared/empty-state";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Textarea } from "@/components/ui/textarea";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { ROUTES } from "@/lib/constants";
import { useMarketplace, type MarketplacePackage, type MarketplaceReview } from "./hooks/use-marketplace";

export function MarketplacePage() {
  const [search, setSearch] = useState("");
  const { packages, installedIDs, loading, refresh, installPackage, likePackage, loadReviews, addReview } = useMarketplace(search);

  const items = useMemo(() => packages, [packages]);

  return (
    <div className="space-y-6 p-4 sm:p-6">
      <PageHeader
        title="Marketplace"
        description="Discover marketplace packages, review metadata, and install them into the local runtime."
        actions={
          <Button variant="outline" size="sm" onClick={refresh} disabled={loading}>
            <RefreshCw className={`mr-2 h-4 w-4 ${loading ? "animate-spin" : ""}`} />
            Refresh
          </Button>
        }
      />

      <SearchInput
        value={search}
        onChange={setSearch}
        placeholder="Search packages, descriptions, or authors"
        className="max-w-md"
      />

      {items.length === 0 ? (
        <EmptyState
          icon={PackageCheck}
          title={search ? "No matching packages" : "No marketplace packages yet"}
          description={search ? "Try a different query." : "Upload or import packages to populate the marketplace."}
        />
      ) : (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {items.map((pkg) => (
            <MarketplaceCard
              key={pkg.id}
              pkg={pkg}
              installed={installedIDs.has(pkg.id)}
              onInstall={() => installPackage(pkg)}
              onLike={() => likePackage(pkg)}
              onLoadReviews={() => loadReviews(pkg.id)}
              onAddReview={(rating, comment) => addReview(pkg, rating, comment)}
              detailHref={`${ROUTES.MARKETPLACE}/${pkg.id}`}
            />
          ))}
        </div>
      )}
    </div>
  );
}

interface MarketplaceCardProps {
  pkg: MarketplacePackage;
  installed: boolean;
  onInstall: () => Promise<{ ok: boolean }>;
  onLike: () => Promise<{ ok: boolean }>;
  onLoadReviews: () => Promise<{ reviews: MarketplaceReview[] }>;
  onAddReview: (rating: number, comment: string) => Promise<{ ok: boolean }>;
  detailHref: string;
}

function MarketplaceCard({ pkg, installed, onInstall, onLike, onLoadReviews, onAddReview, detailHref }: MarketplaceCardProps) {
  const [installing, setInstalling] = useState(false);
  const [liking, setLiking] = useState(false);
  const [reviewsOpen, setReviewsOpen] = useState(false);
  const [reviews, setReviews] = useState<MarketplaceReview[]>([]);
  const [loadingReviews, setLoadingReviews] = useState(false);
  const [rating, setRating] = useState("5");
  const [comment, setComment] = useState("");
  const [submittingReview, setSubmittingReview] = useState(false);
  const requiresEntitlement = !!pkg.pricing?.model && pkg.pricing.model !== "free";
  const installDisabled = installed || installing || requiresEntitlement || pkg.review_state !== "published";

  async function handleInstall() {
    setInstalling(true);
    try {
      await onInstall();
    } finally {
      setInstalling(false);
    }
  }

  async function handleLike() {
    setLiking(true);
    try {
      await onLike();
    } finally {
      setLiking(false);
    }
  }

  async function toggleReviews() {
    const nextOpen = !reviewsOpen;
    setReviewsOpen(nextOpen);
    if (nextOpen && reviews.length === 0) {
      setLoadingReviews(true);
      try {
        const result = await onLoadReviews();
        setReviews(result.reviews ?? []);
      } finally {
        setLoadingReviews(false);
      }
    }
  }

  async function handleSubmitReview() {
    if (!comment.trim()) return;
    setSubmittingReview(true);
    try {
      const result = await onAddReview(Number(rating), comment.trim());
      if (result.ok) {
        const refreshed = await onLoadReviews();
        setReviews(refreshed.reviews ?? []);
        setComment("");
        setReviewsOpen(true);
      }
    } finally {
      setSubmittingReview(false);
    }
  }

  const pricingLabel = pkg.pricing?.model && pkg.pricing.model !== "free"
    ? `${pkg.pricing.model}${pkg.pricing.price ? ` ${pkg.pricing.price}${pkg.pricing.currency ? ` ${pkg.pricing.currency}` : ""}` : ""}`
    : "free";

  return (
    <Card className="gap-4">
      <CardHeader className="gap-3">
        <div className="flex items-start justify-between gap-3">
          <div className="space-y-1">
            <CardTitle>{pkg.name}</CardTitle>
            <CardDescription>{pkg.author || "Unknown author"}</CardDescription>
          </div>
          <Badge variant={installed ? "success" : "outline"}>{installed ? "Installed" : pkg.type}</Badge>
        </div>
        <div className="flex flex-wrap gap-2">
          <Badge variant="outline">v{pkg.version}</Badge>
          <Badge variant={pkg.review_state === "published" ? "success" : "warning"}>
            {pkg.review_state || "pending_review"}
          </Badge>
          <Badge variant="info">{pricingLabel}</Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        <p className="text-sm text-muted-foreground">{pkg.description || "No description provided."}</p>
        <div className="flex flex-wrap gap-4 text-xs text-muted-foreground">
          <span className="inline-flex items-center gap-1">
            <Download className="h-3.5 w-3.5" /> {pkg.downloads ?? 0}
          </span>
          <span className="inline-flex items-center gap-1">
            <Heart className="h-3.5 w-3.5" /> {pkg.likes ?? 0}
          </span>
          <span className="inline-flex items-center gap-1">
            <Star className="h-3.5 w-3.5" /> {pkg.rating?.toFixed(1) ?? "0.0"} ({pkg.rating_count ?? 0})
          </span>
        </div>
        {reviewsOpen && (
          <div className="space-y-3 rounded-lg border p-3">
            <div className="space-y-2">
              {loadingReviews ? (
                <p className="text-sm text-muted-foreground">Loading reviews...</p>
              ) : reviews.length === 0 ? (
                <p className="text-sm text-muted-foreground">No reviews yet.</p>
              ) : (
                reviews.map((review) => (
                  <div key={review.id} className="rounded-md bg-muted/40 p-2">
                    <div className="flex items-center justify-between text-xs text-muted-foreground">
                      <span>{review.user_id}</span>
                      <span>{review.rating}/5</span>
                    </div>
                    <p className="mt-1 text-sm">{review.comment}</p>
                  </div>
                ))
              )}
            </div>
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <label className="text-xs text-muted-foreground" htmlFor={`rating-${pkg.id}`}>Rating</label>
                <select
                  id={`rating-${pkg.id}`}
                  value={rating}
                  onChange={(e) => setRating(e.target.value)}
                  className="rounded-md border bg-background px-2 py-1 text-sm"
                >
                  {[5, 4, 3, 2, 1].map((value) => (
                    <option key={value} value={value}>{value}</option>
                  ))}
                </select>
              </div>
              <Textarea
                value={comment}
                onChange={(e) => setComment(e.target.value)}
                placeholder="Share feedback about this package"
                rows={3}
              />
              <Button size="sm" onClick={handleSubmitReview} disabled={submittingReview || !comment.trim()}>
                {submittingReview ? "Submitting..." : "Submit review"}
              </Button>
            </div>
          </div>
        )}
      </CardContent>
      <CardFooter className="justify-end gap-2">
        <Button asChild variant="ghost" size="sm">
          <Link to={detailHref}>
            Details
            <ArrowRight className="h-4 w-4" />
          </Link>
        </Button>
        <Button variant="outline" size="sm" onClick={handleLike} disabled={liking}>
          <Heart className="mr-2 h-4 w-4" />
          {liking ? "Liking..." : "Like"}
        </Button>
        <Button variant="outline" size="sm" onClick={toggleReviews}>
          <MessageSquarePlus className="mr-2 h-4 w-4" />
          {reviewsOpen ? "Hide reviews" : "Reviews"}
        </Button>
        <Button size="sm" onClick={handleInstall} disabled={installDisabled}>
          <Download className="mr-2 h-4 w-4" />
          {installed ? "Installed" : pkg.review_state !== "published" ? "Not published" : requiresEntitlement ? "Requires purchase" : installing ? "Installing..." : "Install"}
        </Button>
      </CardFooter>
    </Card>
  );
}
