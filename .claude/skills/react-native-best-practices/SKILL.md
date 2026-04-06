---
name: react-native-best-practices
description: "Apply React Native / Expo performance and architecture patterns. Use when implementing or optimizing mobile screens."
context: fork
---

# React Native Best Practices

Apply performance and architecture patterns for: "$ARGUMENTS"

## List Performance (Most Common Issue)

```tsx
// Always FlatList, never ScrollView for lists > 20 items
<FlatList
  data={items}
  keyExtractor={item => item.id}           // unique string, never index
  renderItem={({ item }) => <ItemRow item={item} />}  // must be memoized
  getItemLayout={(_, index) => ({          // set for fixed-height items
    length: ITEM_HEIGHT,
    offset: ITEM_HEIGHT * index,
    index,
  })}
  windowSize={5}
  maxToRenderPerBatch={10}
  initialNumToRender={15}
  removeClippedSubviews={true}
/>

// Memoize list items — prevents re-render on parent update
const ItemRow = React.memo(({ item }: { item: Item }) => (
  <View style={styles.row}>
    <Text>{item.title}</Text>
  </View>
))
```

## Animations (Native Thread = 60fps)

```tsx
// useNativeDriver: true — runs on native thread, never drops frames
Animated.timing(opacity, {
  toValue: 1,
  duration: 300,
  useNativeDriver: true,  // ALWAYS for opacity and transform
}).start()

// useNativeDriver: false — JS thread, causes jank
// Only acceptable for: layout properties (width, height, top, left)

// Reanimated 2 for complex animations
import Animated, { useSharedValue, withTiming, useAnimatedStyle } from 'react-native-reanimated'

const progress = useSharedValue(0)
const animatedStyle = useAnimatedStyle(() => ({ opacity: progress.value }))
progress.value = withTiming(1, { duration: 300 })
```

## Stable Callbacks

```tsx
// Stable reference — prevents FlatList item re-renders
const onItemPress = useCallback((id: string) => {
  router.push(`/detail/${id}`)
}, [])  // empty deps = stable forever

// Stable computed value
const sortedItems = useMemo(() =>
  [...items].sort((a, b) => b.createdAt - a.createdAt),
  [items]
)
```

## Images

```tsx
// expo-image — cached, progressive, placeholder support
import { Image } from 'expo-image'

<Image
  source={{ uri: url }}
  style={{ width: 80, height: 80 }}     // always explicit size
  contentFit="cover"
  placeholder={blurhash}                // low-res placeholder
  transition={200}
/>
```

## State Management

```tsx
// Zustand — simple global state
import { create } from 'zustand'

interface AuthStore {
  user: User | null
  setUser: (user: User | null) => void
}

const useAuthStore = create<AuthStore>((set) => ({
  user: null,
  setUser: (user) => set({ user }),
}))

// React Query — server/async state (lists, detail views)
const { data, isLoading, error } = useQuery({
  queryKey: ['items', filters],
  queryFn: () => fetchItems(filters),
  staleTime: 60_000,
})
```

## Navigation (Expo Router)

```tsx
// File-based routing — app/(tabs)/index.tsx
import { useRouter, useLocalSearchParams } from 'expo-router'

// Typed params
const { id } = useLocalSearchParams<{ id: string }>()
const router = useRouter()
router.push(`/detail/${id}`)

// Screen options in layout, not inside component
// app/(stack)/_layout.tsx
<Stack.Screen name="detail" options={{ headerShown: false }} />
```

## Sensitive Data

```tsx
// SecureStore for tokens/credentials — NOT AsyncStorage
import * as SecureStore from 'expo-secure-store'

await SecureStore.setItemAsync('auth_token', token)
const token = await SecureStore.getItemAsync('auth_token')
```

## Platform Differences

```tsx
import { Platform, StyleSheet } from 'react-native'
import { useSafeAreaInsets } from 'react-native-safe-area-context'

// Platform-conditional
const styles = StyleSheet.create({
  header: {
    paddingTop: Platform.select({ ios: 0, android: 24 }),
    ...Platform.select({
      ios: { shadowColor: '#000', shadowOpacity: 0.1 },
      android: { elevation: 4 },
    }),
  },
})

// Safe areas (notch, home indicator)
const { top, bottom } = useSafeAreaInsets()
```

## Checklist

- [ ] FlatList (not ScrollView) for lists > 20 items
- [ ] `keyExtractor` returns unique string IDs
- [ ] `getItemLayout` set for fixed-height items
- [ ] List items wrapped in `React.memo()`
- [ ] Animations use `useNativeDriver: true`
- [ ] Images use `expo-image` with explicit dimensions
- [ ] Sensitive data in `expo-secure-store`
- [ ] Permissions requested contextually, not on launch
- [ ] `SafeAreaView` / `useSafeAreaInsets` used correctly
