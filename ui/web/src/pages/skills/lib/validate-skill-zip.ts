/** Client-side validation for skill ZIP files before upload.
 * Mirrors server-side checks in internal/http/skills_upload_layout.go and skills_upload.go
 */
import JSZip from "jszip";

export interface SkillZipValidation {
  valid: boolean;
  /** true when ZIP contains multiple top-level skill folders */
  multi?: boolean;
  skillCount?: number;
  name?: string;
  slug?: string;
  description?: string;
  /** i18n key under "upload." namespace */
  error?: string;
  errorDetail?: string;
}

/** Per-skill entry returned by validateMultiSkillZip */
export interface SkillValidationEntry {
  valid: boolean;
  /** Top-level directory name, or "" for root-level SKILL.md */
  dir: string;
  name?: string;
  slug?: string;
  description?: string;
  /** SHA-256 hex digest of the SKILL.md content */
  contentHash?: string;
  /** i18n key under "upload." namespace */
  error?: string;
  errorDetail?: string;
}

export interface MultiSkillZipValidation {
  skills: SkillValidationEntry[];
  /** Top-level ZIP error (corrupt file, not a ZIP, too large) */
  error?: string;
}

const MAX_SKILL_SIZE = 20 * 1024 * 1024; // 20MB
const MAX_SKILLS_PER_ZIP = 50;
const SLUG_REGEX = /^[a-z0-9][a-z0-9-]*[a-z0-9]$/;
const FRONTMATTER_REGEX = /^---\r?\n([\s\S]*?)\r?\n---/;

function normalizeZipPath(p: string): string {
  return p.replace(/\\/g, "/").replace(/^\.\//, "").trim();
}

function zipBasename(p: string): string {
  const parts = normalizeZipPath(p).split("/").filter(Boolean);
  return parts[parts.length - 1] ?? "";
}

type ZipSkillEntry = { path: string; parts: string[] };

function collectSkillEntries(zip: JSZip): ZipSkillEntry[] {
  const out: ZipSkillEntry[] = [];
  for (const key of Object.keys(zip.files)) {
    const f = zip.files[key];
    if (!f || f.dir) continue;
    const n = normalizeZipPath(key);
    if (zipBasename(n) !== "SKILL.md") continue;
    const parts = n.split("/").filter((s) => s.length > 0);
    out.push({ path: n, parts });
  }
  return out;
}

type DiscoveredRoot = { stripPrefix: string; skillKey: string };

/** Same layout rules as discoverSkillZipRoots in skills_upload_layout.go */
function discoverSkillZipRoots(zip: JSZip): { roots: DiscoveredRoot[]; error?: string } {
  const entries = collectSkillEntries(zip);
  if (entries.some((e) => e.parts.length > 3)) {
    return { roots: [], error: "upload.skillMdTooDeep" };
  }
  if (entries.length === 0) {
    return { roots: [], error: "upload.noSkillMd" };
  }

  const rootSkill = entries.find((e) => e.parts.length === 1 && e.parts[0] === "SKILL.md");
  if (rootSkill) {
    if (entries.length > 1) {
      return { roots: [], error: "upload.layoutMixedRoot" };
    }
    return { roots: [{ stripPrefix: "", skillKey: "SKILL.md" }] };
  }

  const depth2 = entries.filter((e) => e.parts.length === 2 && e.parts[1] === "SKILL.md");
  const depth3 = entries.filter((e) => e.parts.length === 3 && e.parts[2] === "SKILL.md");
  if (depth2.length > 0 && depth3.length > 0) {
    return { roots: [], error: "upload.layoutMixedDepth" };
  }

  if (depth2.length > 0) {
    const dirs = [...new Set(depth2.map((e) => e.parts[0]))].sort();
    const roots = dirs.map((d) => ({ stripPrefix: `${d}/`, skillKey: `${d}/SKILL.md` }));
    return { roots };
  }

  if (depth3.length === 0) {
    return { roots: [], error: "upload.noSkillMd" };
  }

  const wraps = [...new Set(depth3.map((e) => e.parts[0]))];
  if (wraps.length > 1) {
    return { roots: [], error: "upload.layoutMultipleWrappers" };
  }
  const w = wraps[0]!;
  const children = [...new Set(depth3.filter((e) => e.parts[0] === w).map((e) => e.parts[1]))].sort();
  const roots = children.map((c) => ({
    stripPrefix: `${w}/${c}/`,
    skillKey: `${w}/${c}/SKILL.md`,
  }));
  return { roots };
}

/**
 * Validate a skill ZIP file client-side — backward-compatible single-skill path.
 * Delegates to validateMultiSkillZip and returns the first skill's result.
 */
export async function validateSkillZip(file: File): Promise<SkillZipValidation> {
  const multi = await validateMultiSkillZip(file);
  if (multi.error) return { valid: false, error: multi.error };
  const first = multi.skills[0];
  if (!first) return { valid: false, error: "upload.noSkillMd" };
  const bundle = multi.skills.length > 1;
  return {
    valid: first.valid,
    multi: bundle ? true : undefined,
    skillCount: bundle ? multi.skills.length : undefined,
    name: first.name,
    slug: first.slug,
    description: first.description,
    error: first.error,
    errorDetail: first.errorDetail,
  };
}

/**
 * Validate a ZIP that may contain one or multiple skills.
 * Returns one SkillValidationEntry per detected SKILL.md, each with contentHash.
 */
export async function validateMultiSkillZip(file: File): Promise<MultiSkillZipValidation> {
  if (!file.name.toLowerCase().endsWith(".zip")) {
    return { skills: [], error: "upload.onlyZip" };
  }
  if (file.size > MAX_SKILL_SIZE) {
    return { skills: [], error: "upload.tooLarge" };
  }

  let zip: JSZip;
  try {
    zip = await JSZip.loadAsync(file);
  } catch {
    return { skills: [], error: "upload.invalidZip" };
  }

  const { roots, error } = discoverSkillZipRoots(zip);
  if (error) {
    return { skills: [], error };
  }
  if (roots.length === 0) {
    return { skills: [], error: "upload.noSkillMd" };
  }
  if (roots.length > MAX_SKILLS_PER_ZIP) {
    return { skills: [], error: "upload.tooManySkills" };
  }

  const skills = await Promise.all(
    roots.map(async (root) => {
      const dir = root.stripPrefix.replace(/\/$/, "");
      const f = zip.files[root.skillKey];
      if (!f || f.dir) {
        return validateSkillEntry(dir, "");
      }
      const content = await f.async("string");
      return validateSkillEntry(dir, content);
    }),
  );

  return { skills };
}

async function validateSkillEntry(dir: string, content: string): Promise<SkillValidationEntry> {
  if (!content.trim()) {
    return { valid: false, dir, error: "upload.emptySkillMd" };
  }

  const match = content.match(FRONTMATTER_REGEX);
  if (!match?.[1]) {
    return { valid: false, dir, error: "upload.noFrontmatter" };
  }

  const fields = parseFrontmatterFields(match[1]);
  if (!fields.name) {
    return { valid: false, dir, error: "upload.nameRequired" };
  }

  const slug = fields.slug ?? slugify(fields.name);
  if (!SLUG_REGEX.test(slug)) {
    return { valid: false, dir, error: "upload.invalidSlug", errorDetail: slug };
  }

  const contentHash = await hashContent(content);

  return {
    valid: true,
    dir,
    name: fields.name,
    slug,
    description: fields.description,
    contentHash,
  };
}

/** SHA-256 hex digest using Web Crypto API */
async function hashContent(content: string): Promise<string> {
  const data = new TextEncoder().encode(content);
  const buf = await crypto.subtle.digest("SHA-256", data);
  return Array.from(new Uint8Array(buf))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

function parseFrontmatterFields(raw: string): Record<string, string> {
  const fields: Record<string, string> = {};
  for (const line of raw.split(/\r?\n/)) {
    const idx = line.indexOf(":");
    if (idx > 0) {
      const key = line.slice(0, idx).trim();
      const val = line
        .slice(idx + 1)
        .trim()
        .replace(/^["']|["']$/g, "");
      if (key && val) fields[key] = val;
    }
  }
  return fields;
}

function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}
