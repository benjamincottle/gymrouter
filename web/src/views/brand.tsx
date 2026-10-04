/** The gym brand's small logo, or an empty slot so names line up. */
export function BrandLogo({ brand }: { brand?: string }) {
  return brand && /^[a-z0-9-]+$/.test(brand) ? (
    <img class="brand" src={`/brands/${brand}.png`} alt="" width={32} height={32} />
  ) : (
    <span class="brand" aria-hidden="true" />
  )
}
