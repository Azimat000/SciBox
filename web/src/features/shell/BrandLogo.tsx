import { productName } from '../../config/product'
import './BrandLogo.css'

export function BrandLogo() {
  return <img className="brand-logo" src="/brand/ion-logo.svg" alt={productName} width="1376" height="548" />
}
