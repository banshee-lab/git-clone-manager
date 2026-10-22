import { getMe, getMeta, ApiError } from "#lib/api/client";
import type { LayoutLoad } from "./$types";

export const ssr = false;
export const prerender = false;

export const load: LayoutLoad = async ({ fetch, url }) => {
  try {
    const context = {
      fetcher: fetch,
      next: `${url.pathname}${url.search}`,
    };
    const [user, meta] = await Promise.all([getMe(context), getMeta(context)]);
    return { user, meta, bootstrapError: null };
  } catch (error) {
    return {
      user: null,
      meta: null,
      bootstrapError:
        error instanceof ApiError
          ? error.message
          : "The Git Clone Manager server could not be reached.",
    };
  }
};
