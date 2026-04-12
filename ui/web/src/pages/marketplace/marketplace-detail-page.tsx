import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router";
import { ArrowLeft, Download, Heart, MessageSquarePlus, RefreshCw, Star } from "lucide-react";
import { DetailPageSkeleton } from "@/components/shared/loading-skeleton";
import { PageHeader } from "@/components/shared/page-header";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { Textarea } from "@/components/ui/textarea";
import { ROUTES } from "@/lib/constants";
import { type MarketplaceReview, useMarketplacePackage } from "./hooks/use-marketplace";

export function MarketplaceDetailPage() {
  const { id } = useParams();
  const { pkg, installed, loading, refresh, installPackage, likePackage, loadReviews, addReview } = useMarketplacePackage(id);
  const [reviews, setReviews] = useState<MarketplaceReview[]>([]);
  const [loadingReviews, setLoadingReviews] = useState(false);
  const [rating, setRating] = useState("5");
  const [comment, setComment] = useState("");
  const [submittingReview, setSubmittingReview] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [liking, setLiking] = useState(false);

  useEffect(() => {
    if (!pkg?.id) {
      setReviews([]);
      return;
    }
    let cancelled = false;
    setLoadingReviews(true);
    loadReviews(pkg.id)
      .then((result) => {
        if (!cancelled) {
          setReviews(result.reviews ?? []);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setReviews([]);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoadingReviews(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [loadReviews, pkg?.id]);

  const pricingLabel = useMemo(() => {
    if (!pkg?.pricing?.model || pkg.pricing.model === "free") {
      return "free";
    }
    const price = pkg.pricing.price ? ` ${pkg.pricing.price}` : "";
    const currency = pkg.pricing.currency ? ` ${pkg.pricing.currency}` : "";
    return `${pkg.pricing.model}${price}${currency}`;
  }, [pkg]);

  if (loading) {
    return <DetailPageSkeleton tabs={2} />;
  }

  if (!pkg) {
    return (
      <div className="space-y-6 p-4 sm:p-6">
        <PageHeader
          title="Marketplace package"
          description="The requested package could not be found or is not visible with your current role."
        />
        <Alert>
          <AlertTitle>Package unavailable</AlertTitle>
          <AlertDescription>
            Go back to the marketplace list and pick another published package.
          </AlertDescription>
        </Alert>
        <Button asChild variant="outline">
          <Link to={ROUTES.MARKETPLACE}>
            <ArrowLeft className="mr-2 h-4 w-4" />
            Back to marketplace
          </Link>
        </Button>
      </div>
    );
  }

  const p = pkg;

  const requiresEntitlement = !!p.pricing?.model && p.pricing.model !== "free";
  const installDisabled = installed || installing || requiresEntitlement || p.review_state !== "published";

  async function handleInstall() {
    setInstalling(true);
    try {
      await installPackage(p);
    } finally {
      setInstalling(false);
    }
  }

  async function handleLike() {
    setLiking(true);
    try {
      await likePackage(p);
      await refresh();
    } finally {
      setLiking(false);
    }
  }

  async function handleReviewSubmit() {
    if (!comment.trim()) return;
    setSubmittingReview(true);
    try {
      const result = await addReview(p, Number(rating), comment.trim());
      if (result.ok) {
        const refreshed = await loadReviews(p.id);
        setReviews(refreshed.reviews ?? []);
        setComment("");
        await refresh();
      }
    } finally {
      setSubmittingReview(false);
    }
  }

  return (
    <div className="space-y-6 p-4 sm:p-6">
      <PageHeader
        title={p.name}
        description={p.description || "Marketplace package details"}
        actions={
          <div className="flex items-center gap-2">
            <Button asChild variant="outline" size="sm">
              <Link to={ROUTES.MARKETPLACE}>
                <ArrowLeft className="mr-2 h-4 w-4" />
                Back
              </Link>
            </Button>
            <Button variant="outline" size="sm" onClick={refresh} disabled={loading || loadingReviews}>
              <RefreshCw className={`mr-2 h-4 w-4 ${loading || loadingReviews ? "animate-spin" : ""}`} />
              Refresh
            </Button>
          </div>
        }
      />

      <div className="grid gap-6 lg:grid-cols-[minmax(0,2fr)_minmax(320px,1fr)]">
        <Card>
          <CardHeader className="gap-3">
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant={installed ? "success" : "outline"}>{installed ? "Installed" : p.type}</Badge>
              <Badge variant={p.review_state === "published" ? "success" : "warning"}>
                {p.review_state || "pending_review"}
              </Badge>
              <Badge variant="info">{pricingLabel}</Badge>
              <Badge variant="outline">v{p.version}</Badge>
              {p.ref ? <Badge variant="outline">ref {p.ref}</Badge> : null}
            </div>
            <CardDescription>
              Published by {p.author || "Unknown author"}
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-3">
              <MetricCard label="Downloads" value={String(p.downloads ?? 0)} />
              <MetricCard label="Likes" value={String(p.likes ?? 0)} />
              <MetricCard label="Rating" value={`${p.rating?.toFixed(1) ?? "0.0"} / 5`} />
            </div>

            <Separator />

            <section className="space-y-2">
              <h2 className="text-sm font-medium">Description</h2>
              <p className="text-sm text-muted-foreground">
                {p.description || "No description provided."}
              </p>
            </section>

            <section className="space-y-2">
              <h2 className="text-sm font-medium">Compatibility</h2>
              <div className="flex flex-wrap gap-2">
                <Badge variant="outline">Type: {p.type}</Badge>
                {p.pricing?.model ? <Badge variant="outline">Billing: {p.pricing.model}</Badge> : null}
                {p.author ? <Badge variant="outline">Author: {p.author}</Badge> : null}
              </div>
            </section>
          </CardContent>
          <CardFooter className="justify-end gap-2">
            <Button variant="outline" size="sm" onClick={handleLike} disabled={liking}>
              <Heart className="mr-2 h-4 w-4" />
              {liking ? "Liking..." : "Like"}
            </Button>
            <Button size="sm" onClick={handleInstall} disabled={installDisabled}>
              <Download className="mr-2 h-4 w-4" />
              {installed ? "Installed" : p.review_state !== "published" ? "Not published" : requiresEntitlement ? "Requires purchase" : installing ? "Installing..." : "Install"}
            </Button>
          </CardFooter>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <MessageSquarePlus className="h-4 w-4" />
              Reviews
            </CardTitle>
            <CardDescription>
              Read feedback from other users and leave your own rating.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="space-y-3">
              {loadingReviews ? (
                <p className="text-sm text-muted-foreground">Loading reviews...</p>
              ) : reviews.length === 0 ? (
                <p className="text-sm text-muted-foreground">No reviews yet.</p>
              ) : (
                reviews.map((review) => (
                  <div key={review.id} className="rounded-md border bg-muted/30 p-3">
                    <div className="flex items-center justify-between text-xs text-muted-foreground">
                      <span>{review.user_id}</span>
                      <span className="inline-flex items-center gap-1">
                        <Star className="h-3.5 w-3.5" />
                        {review.rating}/5
                      </span>
                    </div>
                    <p className="mt-2 text-sm">{review.comment}</p>
                  </div>
                ))
              )}
            </div>

            <Separator />

            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <label className="text-xs text-muted-foreground" htmlFor="marketplace-review-rating">
                  Rating
                </label>
                <select
                  id="marketplace-review-rating"
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
                placeholder="Share what worked well, what is missing, and how this package behaved locally."
                rows={5}
              />
              <Button onClick={handleReviewSubmit} disabled={submittingReview || !comment.trim()}>
                {submittingReview ? "Submitting..." : "Submit review"}
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function MetricCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg border bg-muted/20 p-3">
      <p className="text-xs text-muted-foreground">{label}</p>
      <p className="mt-1 text-lg font-semibold">{value}</p>
    </div>
  );
}
