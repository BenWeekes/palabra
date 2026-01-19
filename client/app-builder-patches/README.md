# App Builder Patches

These patches modify the core App Builder code to support Palabra translation features.

## Patches Included

### 1. filter-translation-uids.patch
Filters UIDs 3000-4999 from rendering as video tiles. These UIDs are used for:
- Palabra translation streams (3000-3099)
- Anam avatar streams (4000-4499)
- Bot audio forwarders (4500-4999)

## How to Apply

After cloning/setting up App Builder, apply the patches:

### Option A: Copy the modified file (recommended)
```bash
cp /home/ubuntu/palabra/client/app-builder-patches/VideoComponent.tsx \
   /home/ubuntu/palabra/app-builder/template/src/pages/video-call/VideoComponent.tsx
```

### Option B: Apply the patch
```bash
cd /home/ubuntu/palabra/app-builder
git apply ../client/app-builder-patches/filter-translation-uids.patch
```

## What the patch does

In `template/src/pages/video-call/VideoComponent.tsx`:

```typescript
// PALABRA FIX: Filter out translation UIDs (3000-4999) from rendering
// These UIDs are used for translation streams and should not appear as tiles
const filteredActiveUids = activeUids.filter((uid) => {
  const uidNum = typeof uid === 'string' ? parseInt(uid, 10) : uid;
  return uidNum < 3000 || uidNum >= 5000;
});
```

Then uses `filteredActiveUids` instead of `activeUids` when rendering tiles.

## After applying

Rebuild the frontend:
```bash
cd /home/ubuntu/palabra
./scripts/build-frontend.sh
```

Deploy:
```bash
sudo cp -r app-builder/Builds/web/* /var/www/palabra/
sudo chown -R www-data:www-data /var/www/palabra
```
