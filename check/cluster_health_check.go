package check

import (
	"fmt"

	"github.com/optiopay/kafka/proto"
)

// periodically checks health of the Kafka cluster
func (check *HealthCheck) checkClusterHealth(metadata *proto.MetadataResp, zkTopics []ZkTopic, zkBrokers []int32) ClusterStatus {
	clusterStatus := ClusterStatus{Status: green}
	mStatus := check.checkBrokerMetadata(metadata, zkBrokers, &clusterStatus)
	tStatus := check.checkTopics(metadata, zkTopics, &clusterStatus)
	clusterStatus.Status = worstStatus(tStatus, mStatus)

	return clusterStatus
}

func (check *HealthCheck) checkBrokerMetadata(metadata *proto.MetadataResp, zkBrokers []int32, cluster *ClusterStatus) (status string) {
	status = green

	var brokersFromMeta []int32
	for _, broker := range metadata.Brokers {
		brokersFromMeta = append(brokersFromMeta, broker.NodeID)
	}

	for _, broker := range brokersFromMeta {
		if !contains(zkBrokers, broker) {
			cluster.Metadata = append(cluster.Metadata, BrokerMetadata{broker, red, "Missing in ZooKeeper"})
			status = red
		}
	}

	for _, broker := range zkBrokers {
		if !contains(brokersFromMeta, broker) {
			cluster.Metadata = append(cluster.Metadata, BrokerMetadata{broker, red, "Missing in metadata"})
			status = red
		}
	}

	return
}

func (check *HealthCheck) checkTopics(metadata *proto.MetadataResp, zkTopics []ZkTopic, cluster *ClusterStatus) (status string) {

	zkTopicMap := make(map[string]ZkTopic)
	for _, topic := range zkTopics {
		zkTopicMap[topic.Name] = topic
	}

	status = green
	for _, topic := range metadata.Topics {
		zkTopic, ok := zkTopicMap[topic.Name]
		topicStatus := TopicStatus{Topic: topic.Name, Status: green, Partitions: make(map[string]PartitionStatus)}
		if !ok {
			topicStatus.Status = red
			topicStatus.ZooKeeper = "Missing ZooKeeper metadata"
		}

		zkPartitionMap := make(map[int32]ZkPartition)
		if ok {
			for _, partition := range zkTopic.Partitions {
				zkPartitionMap[partition.ID] = partition
			}
		}

		for _, partition := range topic.Partitions {
			pStatus := checkPartition(&partition, zkPartitionMap, &topicStatus)
			topicStatus.Status = worstStatus(topicStatus.Status, pStatus)
		}

		if topicStatus.Status != green {
			cluster.Topics = append(cluster.Topics, topicStatus)
			status = worstStatus(topicStatus.Status, status)
		}
	}

	return
}

func checkPartition(partition *proto.MetadataRespPartition, zkPartitionMap map[int32]ZkPartition, topicStatus *TopicStatus) string {
	status := PartitionStatus{Status: green}

	replicas := partition.Replicas

	zkPartition, ok := zkPartitionMap[partition.ID]
	if !ok {
		status.Status = red
		status.ZooKeeper = "Missing ZooKeeper metadata"
	} else {
		replicas = zkPartition.Replicas
	}

	if len(partition.Isrs) < len(replicas) {
		for _, replica := range replicas {
			if !contains(partition.Isrs, replica) {
				status.OutOfSyncReplicas = append(status.OutOfSyncReplicas, replica)
			}
		}
		status.Status = yellow // partition is under-replicated.
	}
	if len(partition.Isrs) == 0 {
		status.Status = red // partition is offline.
	}
	if status.Status != green {
		ID := fmt.Sprintf("%d", partition.ID)
		topicStatus.Partitions[ID] = status
	}

	return status.Status
}

func worstStatus(s1 string, s2 string) string {
	switch s1 {
	case green:
		return s2
	case yellow:
		if s2 == green {
			return s1
		}
		return s2
	case red:
		return s1
	}
	return s2
}
