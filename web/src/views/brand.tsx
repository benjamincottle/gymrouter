import { Shoe } from './icons.tsx'

/** The gym brand's small logo, or for a gym without one (one you added yourself) the shoe that stands for any gym. */
export function BrandLogo({ brand }: { brand?: string }) {
  return brand && /^[a-z0-9-]+$/.test(brand) ? (
    <img class="brand" src={`/brands/${brand}.png`} alt="" width={32} height={32} />
  ) : (
    <svg class="brand" viewBox="0 0 24 24" aria-hidden="true">
      <Shoe />
    </svg>
  )
}
